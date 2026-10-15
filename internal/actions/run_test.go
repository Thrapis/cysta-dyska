package actions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cysta-dyska/internal/testutil"
)

// Proves: permanent delete removes folders with read-only files, and does
// not reach through a junction into the folder it points at.
func TestRemove(t *testing.T) {
	testutil.RequireWindows(t)
	root := t.TempDir()
	write(t, root, map[string]int{"t/a.bin": 10, "t/s/b.bin": 10, "keep/k.bin": 10})
	ro := filepath.Join(root, "t", "a.bin")
	if err := os.Chmod(ro, 0o444); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, "t", "j"), filepath.Join(root, "keep")).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}

	if err := Remove([]string{filepath.Join(root, "t")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "t")); !os.IsNotExist(err) {
		t.Error("target still exists")
	}
	if _, err := os.Stat(filepath.Join(root, "keep", "k.bin")); err != nil {
		t.Errorf("file behind the junction was deleted: %v", err)
	}
}

// Proves: admin commands only run programs from System32, so a lookalike
// earlier on PATH can never be elevated; programs found in the current
// directory are refused.
func TestResolveProgram(t *testing.T) {
	testutil.RequireWindows(t)
	sys := os.Getenv("SystemRoot")
	if p, err := ResolveProgram("whoami", true, sys); err != nil || !strings.EqualFold(filepath.Dir(p), filepath.Join(sys, "System32")) {
		t.Errorf("whoami = %q, %v", p, err)
	}
	if _, err := ResolveProgram("go", true, sys); err == nil {
		t.Error("go (outside System32) accepted as an admin program")
	}
	if _, err := ResolveProgram("go", false, sys); err != nil {
		t.Errorf("go not found: %v", err)
	}
	if _, err := ResolveProgram("definitely-not-a-program-xyz", false, sys); err == nil {
		t.Error("missing program accepted")
	}

	// A lookalike first on PATH must never be chosen for an admin command.
	fake := t.TempDir()
	write(t, fake, map[string]int{"whoami.exe": 10})
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	if p, err := ResolveProgram("whoami", true, sys); err != nil || !strings.EqualFold(filepath.Dir(p), filepath.Join(sys, "System32")) {
		t.Errorf("admin whoami with a lookalike on PATH = %q, %v", p, err)
	}

	// A program in the working directory must not be picked up.
	dir := t.TempDir()
	write(t, dir, map[string]int{"evil.exe": 10})
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(dir)
	if _, err := ResolveProgram("evil", false, sys); err == nil {
		t.Error("program from the current directory accepted")
	}
}

// Proves: commands run without a shell, report output and exit code, and
// get no input (so an interactive prompt cannot hang the app).
func TestRunCommand(t *testing.T) {
	testutil.RequireWindows(t)
	whoami, err := ResolveProgram("whoami", false, os.Getenv("SystemRoot"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := RunCommand(context.Background(), whoami, nil, "")
	if err != nil || res.ExitCode != 0 || strings.TrimSpace(res.Output) == "" {
		t.Errorf("whoami = %+v, %v", res, err)
	}

	cmd, _ := ResolveProgram("cmd", false, os.Getenv("SystemRoot"))
	res, err = RunCommand(context.Background(), cmd, []string{"/c", "exit", "3"}, "")
	if err != nil || res.ExitCode != 3 {
		t.Errorf("exit code = %+v, %v; want 3 and no error", res, err)
	}
	// set /p waits for input; with no stdin it must return at once.
	res, err = RunCommand(context.Background(), cmd, []string{"/c", "set", "/p", "x=Continue?"}, "")
	if err != nil {
		t.Errorf("prompt command: %v", err)
	}
}

// Proves: uninstall strings from the registry are split into program and
// arguments in all the forms installers use.
func TestSplitCommandLine(t *testing.T) {
	cases := []struct{ in, file, params string }{
		{`"C:\Program Files\App\unins000.exe"`, `C:\Program Files\App\unins000.exe`, ``},
		{`"C:\Program Files\App\uninst.exe" /S --x`, `C:\Program Files\App\uninst.exe`, `/S --x`},
		{`MsiExec.exe /X{1234-ABCD}`, `MsiExec.exe`, `/X{1234-ABCD}`},
		{`C:\Program Files\My App\Uninstall.exe`, `C:\Program Files\My App\Uninstall.exe`, ``},
		{`C:\PROGRA~1\App\Uninstall.EXE --uninstall`, `C:\PROGRA~1\App\Uninstall.EXE`, `--uninstall`},
		{`rundll32.exe dfshim.dll,ShArpMaintain app.application`, `rundll32.exe`, `dfshim.dll,ShArpMaintain app.application`},
	}
	for _, c := range cases {
		f, p, err := SplitCommandLine(c.in)
		if err != nil || f != c.file || p != c.params {
			t.Errorf("SplitCommandLine(%q) = %q, %q, %v; want %q, %q", c.in, f, p, err, c.file, c.params)
		}
	}
	for _, bad := range []string{``, `   `, `"C:\unterminated`, `not a program at all`} {
		if _, _, err := SplitCommandLine(bad); err == nil {
			t.Errorf("SplitCommandLine(%q) accepted", bad)
		}
	}
}
