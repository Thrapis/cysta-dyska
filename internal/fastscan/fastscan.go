// Package fastscan runs the MFT fast scan in an elevated copy of the app.
//
// The app itself never runs as administrator. For a fast scan it starts
// itself again through the UAC prompt with HelperFlag; that helper reads the
// drive's master file table (read-only), writes the result as a new file in
// the app's own folder, and exits. The app reads the file and deletes it.
package fastscan

import (
	"context"
	"crypto/rand"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cysta-dyska/internal/actions"
	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/scan/mft"
)

// HelperFlag is the first argument of a helper run.
const HelperFlag = "--mft-scan"

// Helper exit codes.
const (
	exitOK     = 0
	exitUsage  = 2 // bad arguments: nothing was read or written
	exitFailed = 3 // the scan failed; the error is in the result file
)

var (
	driveRe  = regexp.MustCompile(`^[A-Za-z]:$`)
	resultRe = regexp.MustCompile(`^mft-[0-9a-f]{16}\.bin$`)
)

// result is what the helper writes.
type result struct {
	Err   string
	Bad   int // MFT records that could not be read
	Flat  []scan.Flat
	Steps []Step // helper timings
}

// Step is how long one part of a fast scan took.
type Step struct {
	Name string
	Took time.Duration
}

// ResultDir is the folder results are exchanged in. The helper computes it
// itself; it never trusts a folder passed on its command line.
func ResultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "CystaDyska", "fastscan"), nil
}

// IsHelperCall reports whether the process was started as the helper.
func IsHelperCall(args []string) bool {
	return len(args) > 1 && args[1] == HelperFlag
}

// RunHelper is the helper's main: args are os.Args[2:] (drive and result
// file). It returns the process exit code.
func RunHelper(args []string) int {
	dir, err := ResultDir()
	if err != nil {
		return exitUsage
	}
	return helper(dir, args, readVolume)
}

type readFunc func(ctx context.Context, drive string) ([]scan.Flat, int, []Step, error)

func readVolume(ctx context.Context, drive string) ([]scan.Flat, int, []Step, error) {
	start := time.Now()
	t, err := mft.ReadVolume(ctx, drive)
	if err != nil {
		return nil, 0, nil, err
	}
	steps := []Step{
		{"open volume", t.Open},
		{fmt.Sprintf("read+parse %d MiB, %d records, %d workers", t.Bytes>>20, t.Records, t.Workers), time.Since(start) - t.Open},
		{"  disk reads (summed over workers)", t.Read},
		{"  parsing (summed over workers)", t.Parse},
	}
	treeStart := time.Now()
	flat, err := t.Flat(strings.ToUpper(drive) + `\`)
	steps = append(steps, Step{fmt.Sprintf("tree (%d entries)", len(flat)), time.Since(treeStart)})
	return flat, t.Bad, steps, err
}

func helper(dir string, args []string, read readFunc) int {
	if len(args) != 2 || !driveRe.MatchString(args[0]) || checkResultPath(dir, args[1]) != nil {
		return exitUsage
	}
	flat, bad, steps, err := read(context.Background(), args[0])
	res := result{Flat: flat, Bad: bad, Steps: steps}
	code := exitOK
	if err != nil {
		res, code = result{Err: err.Error()}, exitFailed
	}
	if writeResult(args[1], res) != nil {
		return exitFailed
	}
	return code
}

// checkResultPath accepts only a result file directly inside dir.
func checkResultPath(dir, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		!strings.EqualFold(filepath.Dir(path), filepath.Clean(dir)) ||
		!resultRe.MatchString(filepath.Base(path)) {
		return fmt.Errorf("refusing result path %q", path)
	}
	return nil
}

// writeResult creates path, which must not exist yet: an existing file (or
// link) is never opened, let alone overwritten.
func writeResult(path string, res result) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(res); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// launcher starts the elevated helper with args and waits for it.
type launcher func(args []string) (int, error)

// Run fast-scans drive (e.g. "C:") through the elevated helper. The user
// sees a UAC prompt; if they decline, the error wraps actions.ErrDeclined.
func Run(ctx context.Context, drive string) (*scan.Node, error) {
	root, _, err := RunTimed(ctx, drive)
	return root, err
}

// RunTimed is Run that also reports how long each step took.
func RunTimed(ctx context.Context, drive string) (*scan.Node, []Step, error) {
	dir, err := ResultDir()
	if err != nil {
		return nil, nil, err
	}
	return runTimed(ctx, dir, drive, actions.RunSelfElevated)
}

func run(ctx context.Context, dir, drive string, launch launcher) (*scan.Node, error) {
	root, _, err := runTimed(ctx, dir, drive, launch)
	return root, err
}

func runTimed(ctx context.Context, dir, drive string, launch launcher) (*scan.Node, []Step, error) {
	if !driveRe.MatchString(drive) {
		return nil, nil, fmt.Errorf("the fast scan works on whole drives, not %q", drive)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	removeStale(dir)

	var id [8]byte
	rand.Read(id[:])
	path := filepath.Join(dir, "mft-"+hex.EncodeToString(id[:])+".bin")
	defer os.Remove(path)

	type outcome struct {
		code int
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		code, err := launch([]string{HelperFlag, strings.ToUpper(drive), path})
		done <- outcome{code, err}
	}()
	var o outcome
	waitStart := time.Now()
	select {
	case <-ctx.Done():
		// An elevated process cannot be stopped from here; its result
		// file is removed by the next fast scan.
		return nil, nil, ctx.Err()
	case o = <-done:
	}
	wait := time.Since(waitStart)
	if o.err != nil {
		return nil, nil, o.err
	}
	if o.code == exitUsage {
		return nil, nil, errors.New("the fast scan helper refused to run (is this account an administrator?)")
	}

	decodeStart := time.Now()
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("the fast scan helper gave no result (exit code %d)", o.code)
	}
	var res result
	err = gob.NewDecoder(f).Decode(&res)
	f.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("could not read the fast scan result: %w", err)
	}
	if res.Err != "" {
		return nil, nil, errors.New(res.Err)
	}
	steps := append([]Step{{"helper total incl. UAC prompt", wait}}, res.Steps...)
	steps = append(steps, Step{"decode result", time.Since(decodeStart)})
	buildStart := time.Now()
	root, err := scan.Build(res.Flat)
	steps = append(steps, Step{"build scan tree", time.Since(buildStart)})
	return root, steps, err
}

// removeStale deletes result files left by earlier runs. Only names the
// helper writes are touched.
func removeStale(dir string) {
	clearResults(dir)
}

// ClearResults deletes leftover result files from the result folder.
func ClearResults() error {
	dir, err := ResultDir()
	if err != nil {
		return err
	}
	return clearResults(dir)
}

func clearResults(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var first error
	for _, e := range entries {
		if e.Type().IsRegular() && resultRe.MatchString(e.Name()) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}
