// Package app is the thin facade bound to the Wails frontend. It owns the
// current scan, streams progress as events and serves the tree lazily.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"cysta-dyska/internal/actions"
	"cysta-dyska/internal/attribute"
	"cysta-dyska/internal/fastscan"
	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/sources/locations"
	"cysta-dyska/internal/sources/icons"
	"cysta-dyska/internal/sources/registry"
	"cysta-dyska/internal/store"
)

// Event names sent to the frontend.
const (
	EventProgress  = "scan:progress"
	EventDone      = "scan:done"
	EventError     = "scan:error"
	EventCancelled = "scan:cancelled"
	EventDeclined  = "scan:declined" // the UAC prompt for a fast scan was declined
)

// maxChildren caps how many entries Children returns for one directory; the
// remainder is folded into a single summary row.
const maxChildren = 500

// Emitter sends an event to the frontend.
type Emitter interface {
	Emit(name string, data any)
}

// NodeDTO is what the frontend sees for one row of the tree.
type NodeDTO struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"isDir"`
	Size        int64  `json:"size"`  // logical bytes
	Alloc       int64  `json:"alloc"` // bytes used on disk
	Files       int64  `json:"files"`
	Dirs        int64  `json:"dirs"`
	Err         string `json:"err,omitempty"`
	Link        bool   `json:"link"`     // junction/symlink, not followed
	Hardlink    bool   `json:"hardlink"` // disk space counted at another path
	HasChildren bool   `json:"hasChildren"`
	IsRest      bool   `json:"isRest"` // summary row for entries beyond maxChildren
}

// ProgressDTO is the payload of EventProgress.
type ProgressDTO struct {
	Files   int64  `json:"files"`
	Dirs    int64  `json:"dirs"`
	Bytes   int64  `json:"bytes"`
	Errors  int64  `json:"errors"`
	Path    string `json:"path"`
	Elapsed int64  `json:"elapsedMs"`
}

// App holds the state of the current and last completed scan.
type App struct {
	emit Emitter

	// Sources for attribution; replaced in tests.
	getenv        func(string) string
	installedApps func() ([]registry.App, error)
	appIcon       func(path string, index int) (string, error)

	history         *store.Store // nil when the history database could not be opened
	historyOpts     store.Options
	hotspotMinDelta int64

	fastScan         func(ctx context.Context, drive string) (*scan.Node, error) // replaced in tests
	clearFastResults func() error                                                // replaced in tests

	goos string // runtime.GOOS; replaced in tests

	exec     executors  // do the actual cleanup work; replaced in tests
	actionMu sync.Mutex // one action at a time

	mu             sync.Mutex
	cancel         context.CancelFunc // non-nil while a scan is running
	tree           *scan.Node         // last completed scan
	culprits       *CulpritsDTO       // attribution of tree
	snapshotID     int64              // history id of tree; 0 if not saved
	method         string             // store.MethodWalk or store.MethodMFT: how tree was made
	historyWarning string
	findings       []attribute.Finding // behind culprits.Items, same order
	vars           locations.Vars      // this machine's folders at scan time
	plans          map[string]*plan    // planned actions waiting for RunAction
}

// New returns an App that reports events through emit and saves finished
// scans to history, which may be nil.
func New(emit Emitter, history *store.Store) *App {
	return &App{
		emit: emit, getenv: os.Getenv, installedApps: registry.Installed, appIcon: icons.DataURI,
		history: history, historyOpts: store.DefaultOptions, hotspotMinDelta: defaultHotspotMinDelta,
		exec: defaultExecutors(), fastScan: fastscan.Run, clearFastResults: fastscan.ClearResults, goos: runtime.GOOS,
	}
}

// Platform tells the frontend which OS it runs on ("windows", "darwin").
func (a *App) Platform() string { return a.goos }

// errWindowsOnly is returned for features that exist only on Windows. On
// macOS the app explains and measures but never changes anything.
var errWindowsOnly = errors.New("this is only available on Windows")

// windowsOnly returns errWindowsOnly unless the app runs on Windows.
func (a *App) windowsOnly() error {
	if a.goos != "windows" {
		return errWindowsOnly
	}
	return nil
}

// StartScan begins scanning root in the background. Results arrive as
// events; it returns an error only if a scan is already running.
func (a *App) StartScan(root string) error {
	return a.start(store.MethodWalk, func(ctx context.Context, progress func(scan.Progress)) (*scan.Node, error) {
		return scan.Scan(ctx, root, scan.Options{OnProgress: progress})
	})
}

var driveRootRe = regexp.MustCompile(`^([A-Za-z]:)\\?$`)

// StartFastScan scans a whole drive (e.g. `C:\`) by reading its master file
// table in an elevated helper; Windows shows a UAC prompt first. Results
// arrive as for StartScan; a declined prompt sends EventDeclined.
func (a *App) StartFastScan(drive string) error {
	if err := a.windowsOnly(); err != nil {
		return err
	}
	m := driveRootRe.FindStringSubmatch(drive)
	if m == nil {
		return fmt.Errorf("the fast scan works on whole drives, not %q", drive)
	}
	d := strings.ToUpper(m[1])
	return a.start(store.MethodMFT, func(ctx context.Context, _ func(scan.Progress)) (*scan.Node, error) {
		return a.fastScan(ctx, d)
	})
}

type scanFunc func(ctx context.Context, progress func(scan.Progress)) (*scan.Node, error)

func (a *App) start(method string, do scanFunc) error {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("a scan is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.mu.Unlock()

	go a.run(ctx, method, do)
	return nil
}

func (a *App) run(ctx context.Context, method string, do scanFunc) {
	start := time.Now()
	// The scanner already paces progress reports (about 10 per second).
	tree, err := do(ctx, func(p scan.Progress) {
		a.emit.Emit(EventProgress, ProgressDTO{
			Files: p.Files, Dirs: p.Dirs, Bytes: p.Bytes, Errors: p.Errors,
			Path: p.Path, Elapsed: time.Since(start).Milliseconds(),
		})
	})

	took := time.Since(start)
	var culprits CulpritsDTO
	var findings []attribute.Finding
	var vars locations.Vars
	var snapshotID int64
	var historyWarning string
	if err == nil {
		culprits, findings, vars = a.attribute(tree)                   // ~0.2 s for a full drive
		snapshotID, historyWarning = a.saveHistory(tree, took, method) // ~10k rows for a full drive
	}

	a.mu.Lock()
	a.cancel = nil
	if err == nil {
		a.tree = tree
		a.culprits = &culprits
		a.findings, a.vars = findings, vars
		a.plans = nil // plans refer to the previous scan
		a.snapshotID, a.historyWarning, a.method = snapshotID, historyWarning, method
	}
	a.mu.Unlock()

	switch {
	case errors.Is(err, context.Canceled):
		a.emit.Emit(EventCancelled, nil)
	case errors.Is(err, actions.ErrDeclined):
		a.emit.Emit(EventDeclined, nil)
	case err != nil:
		a.emit.Emit(EventError, err.Error())
	default:
		a.emit.Emit(EventDone, toDTO(tree))
	}
}

// CancelScan stops the running scan, if any. The previous result is kept.
func (a *App) CancelScan() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// Children returns the entries of the directory at path from the last scan,
// largest first, capped at maxChildren plus one summary row.
func (a *App) Children(path string) ([]NodeDTO, error) {
	a.mu.Lock()
	tree := a.tree
	a.mu.Unlock()
	if tree == nil {
		return nil, errors.New("no scan results yet")
	}
	n := tree.Find(path)
	if n == nil {
		return nil, errors.New("path is not in the last scan")
	}

	out := make([]NodeDTO, 0, min(len(n.Children), maxChildren+1))
	for i, c := range n.Children {
		if i == maxChildren {
			rest := NodeDTO{Name: "", Path: n.Path(), IsRest: true}
			for _, r := range n.Children[i:] {
				rest.Size += r.Size
				rest.Alloc += r.Alloc
				rest.Files += r.Files
				rest.Dirs += r.Dirs
				if r.IsDir {
					rest.Dirs++
				}
			}
			out = append(out, rest)
			break
		}
		out = append(out, toDTO(c))
	}
	return out, nil
}

func toDTO(n *scan.Node) NodeDTO {
	return NodeDTO{
		Name: n.Name, Path: n.Path(), IsDir: n.IsDir,
		Size: n.Size, Alloc: n.Alloc, Files: n.Files, Dirs: n.Dirs, Err: n.Err,
		Link: n.Link, Hardlink: n.Hardlink,
		HasChildren: len(n.Children) > 0,
	}
}
