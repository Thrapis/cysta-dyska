package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxOutput is how much command output is kept (the end of it).
const maxOutput = 64 << 10

// Remove permanently deletes paths. Only used when the user has turned on
// permanent delete; the caller has already checked them with Guard and
// Measure. os.RemoveAll removes junctions without entering them.
func Remove(paths []string) error {
	var errs []error
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ResolveProgram finds a rule's program. Admin programs are always taken
// from System32 and never looked up on PATH, so a lookalike earlier on PATH
// can never be run elevated. Other programs are looked up on PATH; ones
// found through the current directory are refused.
func ResolveProgram(name string, admin bool, systemRoot string) (string, error) {
	if admin {
		if systemRoot == "" || strings.ContainsAny(name, `\/:`) {
			return "", fmt.Errorf("cannot locate Windows' own %s", name)
		}
		p := filepath.Join(systemRoot, "System32", name)
		if filepath.Ext(p) == "" {
			p += ".exe"
		}
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			return "", fmt.Errorf("%s is not one of Windows' own programs", name)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s was not found; is it installed and on PATH?", name)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s resolves to a relative path", name)
	}
	return p, nil
}

// Result is what a command did.
type Result struct {
	ExitCode int
	Output   string // the last maxOutput bytes of stdout and stderr
}

// RunCommand runs program with args and waits. There is no shell and no
// input: a tool that asks a question gets end-of-input and stops.
func RunCommand(ctx context.Context, program string, args []string, dir string) (Result, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	hideWindow(cmd)
	var out tailBuffer
	cmd.Stdout, cmd.Stderr = &out, &out

	err := cmd.Run()
	res := Result{Output: out.String()}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		res.ExitCode = exit.ExitCode() // the tool ran and reported failure
		return res, nil
	case err != nil:
		return res, err
	}
	return res, nil
}

// tailBuffer keeps only the last maxOutput bytes written to it.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > maxOutput {
		t.b = t.b[len(t.b)-maxOutput:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.b) }

// SplitCommandLine splits a registry UninstallString into the program and
// its parameters.
func SplitCommandLine(s string) (file, params string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("empty command")
	}
	if s[0] == '"' {
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			return "", "", errors.New("unterminated quote")
		}
		return s[1 : end+1], strings.TrimSpace(s[end+2:]), nil
	}
	// Unquoted: the program ends at the first ".exe" (paths may have spaces).
	i := strings.Index(strings.ToLower(s), ".exe")
	if i < 0 {
		return "", "", fmt.Errorf("cannot find the program in %q", s)
	}
	return s[:i+4], strings.TrimSpace(s[i+4:]), nil
}
