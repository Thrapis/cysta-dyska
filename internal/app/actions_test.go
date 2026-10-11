package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cysta-dyska/internal/actions"
	"cysta-dyska/internal/sources/registry"
	"cysta-dyska/internal/store"
)

// fakeExec records what the app asked to run instead of touching the real
// Recycle Bin or running real tools.
type fakeExec struct {
	recycled  [][]string
	commands  [][]string
	elevated  [][]string
	uninstall []string
	exitCode  int
}

func newActionApp(t *testing.T, apps []registry.App) (*App, *fakeEmitter, *fakeExec, string) {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, em := newTestApp(root, apps)
	a.history = s
	fx := &fakeExec{}
	a.exec = executors{
		recycle: func(p []string) error { fx.recycled = append(fx.recycled, p); return removeAll(p) },
		remove:  actions.Remove,
		run: func(_ context.Context, prog string, args []string, dir string) (actions.Result, error) {
			fx.commands = append(fx.commands, append([]string{prog, dir}, args...))
			return actions.Result{ExitCode: fx.exitCode, Output: "ok"}, nil
		},
		elevated: func(prog string, args []string) (int, error) {
			fx.elevated = append(fx.elevated, append([]string{prog}, args...))
			return 0, nil
		},
		uninstall: func(s string) error { fx.uninstall = append(fx.uninstall, s); return nil },
		resolve:   func(name string, admin bool, _ string) (string, error) { return `C:\fake\` + name + ".exe", nil },
	}
	return a, em, fx, root
}

func removeAll(paths []string) error {
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

func culprit(t *testing.T, a *App, ruleID string) CulpritDTO {
	t.Helper()
	res, err := a.Culprits()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Items {
		if c.RuleID == ruleID {
			return c
		}
	}
	t.Fatalf("no %s finding in %+v", ruleID, res.Items)
	return CulpritDTO{}
}

func hasAction(c CulpritDTO, kind string) (ActionOptionDTO, bool) {
	for _, o := range c.Actions {
		if o.Kind == kind {
			return o, true
		}
	}
	return ActionOptionDTO{}, false
}

// Proves: each finding offers exactly the actions its rule allows: the
// owner's command, Recycle Bin for opted-in rules, uninstall for programs,
// and permanent delete only after the user turns it on.
func TestActionOptions(t *testing.T) {
	apps := []registry.App{{Name: "Widgetry", UninstallString: `"C:\Program Files\Widgetry\unins000.exe"`}}
	a, em, _, root := newActionApp(t, apps)
	a.installedApps = func() ([]registry.App, error) {
		apps[0].InstallDir = filepath.Join(root, "progs", "Widgetry")
		return apps, nil
	}
	writeFiles(t, root, map[string]int{
		"home/AppData/Local/npm-cache/_cacache/x": 100_000,
		"home/AppData/Roaming/Widgetry/state.db":  100_000,
		"progs/Widgetry/w.exe":                    100_000,
	})
	scanAndWait(t, a, em, root)

	npm := culprit(t, a, "npm-cache")
	if o, ok := hasAction(npm, "command"); !ok || o.Command != "npm cache clean --force" {
		t.Errorf("npm actions = %+v, want its command", npm.Actions)
	}
	if _, ok := hasAction(npm, "recycle"); ok {
		t.Error("npm-cache has no file action in its rule, but recycle was offered")
	}

	data := culprit(t, a, "app-data")
	if o, ok := hasAction(data, "recycle"); !ok || !o.NeedsAck || o.Blocked != "" {
		t.Errorf("app-data recycle = %+v, want offered with acknowledgement", o)
	}
	if _, ok := hasAction(data, "delete"); ok {
		t.Error("delete offered while permanent delete is off")
	}
	if err := a.SetAllowPermanentDelete(true); err != nil {
		t.Fatal(err)
	}
	if _, ok := hasAction(culprit(t, a, "app-data"), "delete"); !ok {
		t.Error("delete not offered after turning it on")
	}

	prog := culprit(t, a, "installed-app")
	if _, ok := hasAction(prog, "uninstall"); !ok {
		t.Errorf("program actions = %+v, want uninstall", prog.Actions)
	}
	if _, ok := hasAction(prog, "recycle"); ok {
		t.Error("recycle offered on an installed program")
	}
}

// Proves: lists the UI reads are sent as [] and never as null. A null
// "actions" list on findings without actions (e.g. Android emulator disks)
// crashed the Culprits view into a black window after a drive scan.
func TestActionListsAreNeverNull(t *testing.T) {
	a, em, _, root := newActionApp(t, nil)
	writeFiles(t, root, map[string]int{
		"home/.android/avd/pixel.avd/disk.img":    100_000, // android-avd: no actions
		"home/AppData/Local/npm-cache/_cacache/x": 100_000, // npm-cache: a command
	})
	scanAndWait(t, a, em, root)

	res, err := a.Culprits()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), `"actions":null`) {
		t.Errorf("culprits JSON has a null actions list: %s", raw)
	}
	if len(res.Items) < 2 {
		t.Fatalf("want both findings, got %+v", res.Items)
	}

	npm := culprit(t, a, "npm-cache")
	plan, err := a.PlanAction(ActionRequest{RuleID: npm.RuleID, Path: npm.Path, Kind: "command"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(plan)
	for _, field := range []string{`"targets":null`, `"warnings":null`} {
		if strings.Contains(string(raw), field) {
			t.Errorf("plan JSON has %s: %s", field, raw)
		}
	}
}

// Proves: a "check first" action does nothing without the acknowledgement;
// with it, the files are recycled and the action is logged with sizes.
func TestRecycleNeedsAckAndIsLogged(t *testing.T) {
	a, em, fx, root := newActionApp(t, []registry.App{{Name: "Widgetry"}})
	writeFiles(t, root, map[string]int{"home/AppData/Roaming/Widgetry/state.db": 100_000})
	scanAndWait(t, a, em, root)
	c := culprit(t, a, "app-data")

	plan, err := a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "recycle"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Bytes < 100_000 || plan.Files != 1 || !plan.NeedsAck || plan.Blocked != "" {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := a.RunAction(plan.ID, false); err == nil {
		t.Fatal("ran without acknowledgement")
	}
	if len(fx.recycled) != 0 {
		t.Fatal("recycled without acknowledgement")
	}

	plan, _ = a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "recycle"})
	res, err := a.RunAction(plan.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "done" || res.Freed < 100_000 || len(fx.recycled) != 1 || fx.recycled[0][0] != c.Path {
		t.Errorf("result = %+v, recycled %v", res, fx.recycled)
	}
	if _, err := a.RunAction(plan.ID, true); err == nil {
		t.Error("the same plan ran twice")
	}

	log, err := a.ActionLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0].Kind != "recycle" || log[0].Outcome != "done" || log[0].BytesBefore < 100_000 {
		t.Errorf("log = %+v", log)
	}
}

// Proves: everything is checked again when Run is pressed: a junction that
// appeared after planning blocks the action.
func TestRunRechecksTargets(t *testing.T) {
	a, em, fx, root := newActionApp(t, []registry.App{{Name: "Widgetry"}})
	writeFiles(t, root, map[string]int{"home/AppData/Roaming/Widgetry/state.db": 100_000, "elsewhere/keep.txt": 10})
	scanAndWait(t, a, em, root)
	c := culprit(t, a, "app-data")
	plan, err := a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "recycle"})
	if err != nil || plan.Blocked != "" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}

	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(c.Path, "j"), filepath.Join(root, "elsewhere")).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}
	if _, err := a.RunAction(plan.ID, true); err == nil {
		t.Error("ran although a junction appeared inside the target")
	}
	if len(fx.recycled) != 0 {
		t.Error("recycled despite the junction")
	}
	if _, err := os.Stat(filepath.Join(root, "elsewhere", "keep.txt")); err != nil {
		t.Errorf("file behind the junction is gone: %v", err)
	}
}

// Proves: owner commands run the rule's exact program and arguments; admin
// ones go through elevation; a failing exit code is logged as failed.
func TestCommandActions(t *testing.T) {
	a, em, fx, root := newActionApp(t, nil)
	writeFiles(t, root, map[string]int{"home/AppData/Local/npm-cache/_cacache/x": 100_000})
	scanAndWait(t, a, em, root)
	c := culprit(t, a, "npm-cache")

	plan, err := a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "command", Suggest: 0})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Display != "npm cache clean --force" || plan.NeedsAck {
		t.Errorf("plan = %+v", plan)
	}
	res, err := a.RunAction(plan.ID, false)
	if err != nil || res.Outcome != "done" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if _, err := a.RunAction(plan.ID, false); err == nil || len(fx.commands) != 1 {
		t.Errorf("the same command plan ran twice (err=%v, runs=%d)", err, len(fx.commands))
	}
	want := []string{`C:\fake\npm.exe`, "", "cache", "clean", "--force"}
	if len(fx.commands) != 1 || len(fx.commands[0]) != len(want) {
		t.Fatalf("commands = %v", fx.commands)
	}
	for i := range want {
		if i != 1 && fx.commands[0][i] != want[i] {
			t.Errorf("command = %v, want %v", fx.commands[0], want)
		}
	}

	fx.exitCode = 2
	plan, _ = a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "command", Suggest: 0})
	res, err = a.RunAction(plan.ID, false)
	if err != nil || res.Outcome != "failed" {
		t.Errorf("exit code 2 gave %+v, %v; want outcome failed", res, err)
	}
	if _, err := a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "admin", Suggest: 0}); err == nil {
		t.Error("a normal command was accepted as admin")
	}
}

// Proves: permanent delete is refused while the setting is off, and works
// (for real, on a temp folder) once it is on.
func TestPermanentDelete(t *testing.T) {
	a, em, _, root := newActionApp(t, []registry.App{{Name: "Widgetry"}})
	writeFiles(t, root, map[string]int{"home/AppData/Roaming/Widgetry/state.db": 100_000})
	scanAndWait(t, a, em, root)
	c := culprit(t, a, "app-data")

	if _, err := a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "delete"}); err == nil {
		t.Fatal("delete planned while the setting is off")
	}
	if err := a.SetAllowPermanentDelete(true); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	// Turning the setting off between plan and run must stop it.
	a.SetAllowPermanentDelete(false)
	if _, err := a.RunAction(plan.ID, true); err == nil {
		t.Fatal("delete ran after the setting was turned off")
	}
	a.SetAllowPermanentDelete(true)
	plan, _ = a.PlanAction(ActionRequest{RuleID: c.RuleID, Path: c.Path, Kind: "delete"})
	res, err := a.RunAction(plan.ID, true)
	if err != nil || res.Outcome != "done" {
		t.Fatalf("delete = %+v, %v", res, err)
	}
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("folder still there: %v", err)
	}
}

// Proves: unknown findings and kinds the rule does not allow are refused.
func TestPlanRefusals(t *testing.T) {
	a, em, _, root := newActionApp(t, nil)
	writeFiles(t, root, map[string]int{"home/AppData/Local/npm-cache/_cacache/x": 100_000})
	scanAndWait(t, a, em, root)
	c := culprit(t, a, "npm-cache")

	for name, req := range map[string]ActionRequest{
		"unknown finding":     {RuleID: "npm-cache", Path: filepath.Join(root, "nope"), Kind: "command"},
		"wrong rule":          {RuleID: "go-build-cache", Path: c.Path, Kind: "command"},
		"recycle not allowed": {RuleID: c.RuleID, Path: c.Path, Kind: "recycle"},
		"no such suggestion":  {RuleID: c.RuleID, Path: c.Path, Kind: "command", Suggest: 5},
		"no uninstaller":      {RuleID: c.RuleID, Path: c.Path, Kind: "uninstall"},
		"unknown kind":        {RuleID: c.RuleID, Path: c.Path, Kind: "shred"},
	} {
		if _, err := a.PlanAction(req); err == nil {
			t.Errorf("%s: planned", name)
		}
	}
}

// Proves (regression): a scan in which nothing is explained sends an empty
// culprits list, not JSON null, which would crash the Culprits view.
func TestEmptyCulpritsAreNotNull(t *testing.T) {
	a, em, _, root := newActionApp(t, nil)
	writeFiles(t, root, map[string]int{"home/Documents/notes.txt": 1000})
	scanAndWait(t, a, em, root)

	res, err := a.Culprits()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), `"items":null`) {
		t.Errorf("culprits JSON has a null items list: %s", raw)
	}
}
