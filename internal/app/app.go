// Package app is the thin facade bound to the Wails frontend. It owns the
// current scan, streams progress as events and serves the tree lazily.
package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"cysta-dyska/internal/scan"
)

// Event names sent to the frontend.
const (
	EventProgress  = "scan:progress"
	EventDone      = "scan:done"
	EventError     = "scan:error"
	EventCancelled = "scan:cancelled"
)

// maxChildren caps how many entries Children returns for one directory; the
// remainder is folded into a single summary row.
const maxChildren = 500

// progressInterval throttles progress events so the UI is not flooded.
const progressInterval = 100 * time.Millisecond

// Emitter sends an event to the frontend.
type Emitter interface {
	Emit(name string, data any)
}

// NodeDTO is what the frontend sees for one row of the tree.
type NodeDTO struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"isDir"`
	Size        int64  `json:"size"`
	Files       int64  `json:"files"`
	Dirs        int64  `json:"dirs"`
	Err         string `json:"err,omitempty"`
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

	mu     sync.Mutex
	cancel context.CancelFunc // non-nil while a scan is running
	tree   *scan.Node         // last completed scan
}

// New returns an App that reports events through emit.
func New(emit Emitter) *App {
	return &App{emit: emit}
}

// StartScan begins scanning root in the background. Results arrive as
// events; it returns an error only if a scan is already running.
func (a *App) StartScan(root string) error {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return errors.New("a scan is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.mu.Unlock()

	go a.run(ctx, root)
	return nil
}

func (a *App) run(ctx context.Context, root string) {
	start := time.Now()
	var lastEmit time.Time
	tree, err := scan.Scan(ctx, root, func(p scan.Progress) {
		if time.Since(lastEmit) < progressInterval {
			return
		}
		lastEmit = time.Now()
		a.emit.Emit(EventProgress, ProgressDTO{
			Files: p.Files, Dirs: p.Dirs, Bytes: p.Bytes, Errors: p.Errors,
			Path: p.Path, Elapsed: time.Since(start).Milliseconds(),
		})
	})

	a.mu.Lock()
	a.cancel = nil
	if err == nil {
		a.tree = tree
	}
	a.mu.Unlock()

	switch {
	case errors.Is(err, context.Canceled):
		a.emit.Emit(EventCancelled, nil)
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
			rest := NodeDTO{Name: "", Path: n.Path, IsRest: true}
			for _, r := range n.Children[i:] {
				rest.Size += r.Size
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
		Name: n.Name, Path: n.Path, IsDir: n.IsDir,
		Size: n.Size, Files: n.Files, Dirs: n.Dirs, Err: n.Err,
		HasChildren: len(n.Children) > 0,
	}
}
