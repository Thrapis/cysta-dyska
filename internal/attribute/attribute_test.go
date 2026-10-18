package attribute

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/sources/locations"
	"cysta-dyska/internal/sources/registry"
	"cysta-dyska/internal/testutil"
)

// Proves: the rule pack shipped in the binary loads and passes validation
// (unique ids, known categories/risks/variables, EN+BE texts everywhere).
func TestEmbeddedPackIsValid(t *testing.T) {
	p, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	min := 30 // the Windows pack; the macOS pack is smaller
	if runtime.GOOS == "darwin" {
		min = 15
	}
	if len(p.Rules) < min {
		t.Errorf("only %d rules loaded, want at least %d", len(p.Rules), min)
	}
}

// Proves: validation rejects each kind of broken rule, so mistakes in the
// rule pack fail tests instead of silently mis-attributing space.
func TestValidationRejectsBrokenRules(t *testing.T) {
	const templates = `
  - {id: installed-app, template: true, category: installed-app, risk: review, explain: {en: a, be: b}}
  - {id: app-data, template: true, category: app-data, risk: review, explain: {en: a, be: b}}
`
	cases := map[string]string{
		"duplicate id":       `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}}` + "\n  - " + `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}}`,
		"unknown category":   `{id: x, owner: o, category: nope, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}}`,
		"unknown risk":       `{id: x, owner: o, category: temp, risk: maybe, paths: ["{TEMP}"], explain: {en: a, be: b}}`,
		"missing be":         `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a}}`,
		"unknown variable":   `{id: x, owner: o, category: temp, risk: safe, paths: ["{NOPE}\\x"], explain: {en: a, be: b}}`,
		"relative path":      `{id: x, owner: o, category: temp, risk: safe, paths: ["temp"], explain: {en: a, be: b}}`,
		"no paths or names":  `{id: x, owner: o, category: temp, risk: safe, explain: {en: a, be: b}}`,
		"typo in field":      `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}, sugest: []}`,
		"bad settings uri":   `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}, suggest: [{text: {en: a, be: b}, settings: "https://x"}]}`,
		"suggest missing be": `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}, suggest: [{text: {en: a}}]}`,
		"name with slash":    `{id: x, owner: o, category: temp, risk: safe, names: [{name: "a/b"}], explain: {en: a, be: b}}`,
	}
	for name, rule := range cases {
		fsys := fstest.MapFS{"r/x.yaml": {Data: []byte("version: 1\nrules:\n  - " + rule + templates)}}
		if _, err := Load(fsys, "r"); err == nil {
			t.Errorf("%s: Load accepted a broken rule", name)
		}
	}

	ok := fstest.MapFS{"r/x.yaml": {Data: []byte("version: 1\nrules:\n  - " + `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}}` + templates)}}
	if _, err := Load(ok, "r"); err != nil {
		t.Errorf("valid pack rejected: %v", err)
	}
	noTemplates := fstest.MapFS{"r/x.yaml": {Data: []byte("version: 1\nrules:\n  - " + `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}}`)}}
	if _, err := Load(noTemplates, "r"); err == nil {
		t.Error("pack without template rules accepted")
	}
}

// Proves: runnable suggestions and file actions are validated so a rule
// can never show one command but run another, run a placeholder command,
// look up a program by path, or offer file actions on Windows items.
func TestValidationOfActions(t *testing.T) {
	const templates = `
  - {id: installed-app, template: true, category: installed-app, risk: review, explain: {en: a, be: b}}
  - {id: app-data, template: true, category: app-data, risk: review, explain: {en: a, be: b}}
`
	base := `{id: x, owner: o, category: temp, risk: safe, paths: ["{TEMP}"], explain: {en: a, be: b}, `
	sugg := func(cmd, run string) string {
		return base + `suggest: [{text: {en: a, be: b}, command: "` + cmd + `", run: ` + run + `}]}`
	}
	bad := map[string]string{
		"shown differs from run":  sugg("go clean -cache", `{program: go, args: [clean, -modcache]}`),
		"placeholder in args":     sugg("wsl --manage <distro>", `{program: wsl, args: [--manage, "<distro>"]}`),
		"program with a path":     sugg(`C:\\x\\go clean`, `{program: "C:\\x\\go", args: [clean]}`),
		"run without command":     base + `suggest: [{text: {en: a, be: b}, run: {program: go, args: [clean]}}]}`,
		"project dir on path rule": sugg("go clean", `{program: go, args: [clean], in: project}`),
		"unknown in":              sugg("go clean", `{program: go, args: [clean], in: home}`),
		"file action on system":   `{id: x, owner: o, category: system, risk: system, paths: ["{TEMP}"], fileAction: folder, explain: {en: a, be: b}}`,
		"file action on never":    `{id: x, owner: o, category: system, risk: never, paths: ["{TEMP}"], fileAction: contents, explain: {en: a, be: b}}`,
		"unknown file action":     base + `fileAction: shred}`,
		"file action on programs": `{id: x, owner: o, category: installed-app, risk: review, paths: ["{TEMP}"], fileAction: folder, explain: {en: a, be: b}}`,
	}
	for name, rule := range bad {
		fsys := fstest.MapFS{"r/x.yaml": {Data: []byte("version: 1\nrules:\n  - " + rule + templates)}}
		if _, err := Load(fsys, "r"); err == nil {
			t.Errorf("%s: Load accepted it", name)
		}
	}

	good := map[string]string{
		"plain run":   sugg("go clean -cache", `{program: go, args: [clean, -cache]}`),
		"admin run":   sugg("powercfg /hibernate off", `{program: powercfg, args: [/hibernate, "off"], admin: true}`),
		"file action": base + `fileAction: contents}`,
		"project run": `{id: x, owner: o, category: build-output, risk: safe, names: [{name: target}], explain: {en: a, be: b}, suggest: [{text: {en: a, be: b}, command: "cargo clean", run: {program: cargo, args: [clean], in: project}}]}`,
	}
	for name, rule := range good {
		fsys := fstest.MapFS{"r/x.yaml": {Data: []byte("version: 1\nrules:\n  - " + rule + templates)}}
		if _, err := Load(fsys, "r"); err != nil {
			t.Errorf("%s: rejected: %v", name, err)
		}
	}
}

// Proves: the shipped rules never offer file actions on Recycle Bin,
// Downloads, browser caches or anything Windows owns.
func TestShippedFileActionsAreConservative(t *testing.T) {
	testutil.RequireWindows(t)
	p := mustPack(t)
	for _, id := range []string{"recycle-bin", "downloads", "chrome-cache", "edge-cache", "firefox-cache",
		"hiberfil", "pagefile", "winsxs", "windows-installer-cache", "installed-app", "docker-desktop-disk", "wsl-disks"} {
		r := p.Rule(id)
		if r == nil {
			t.Errorf("rule %s missing", id)
			continue
		}
		if r.FileAction != FileActionNone {
			t.Errorf("rule %s offers file action %q", id, r.FileAction)
		}
	}
}

// fixture builds a fake user profile in a temp dir and returns the scan
// tree and matching location variables.
func fixture(t *testing.T, files map[string]int) (*scan.Node, locations.Vars) {
	t.Helper()
	root := t.TempDir()
	for name, n := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{
		"USERPROFILE":  filepath.Join(root, "home"),
		"LOCALAPPDATA": filepath.Join(root, "home", "AppData", "Local"),
		"APPDATA":      filepath.Join(root, "home", "AppData", "Roaming"),
		"ProgramData":  filepath.Join(root, "ProgramData"),
		"ProgramFiles": filepath.Join(root, "Program Files"),
		"SystemRoot":   filepath.Join(root, "Windows"),
		"TEMP":         filepath.Join(root, "home", "AppData", "Local", "Temp"),
	}
	vars := locations.Resolve(func(k string) string { return env[k] })
	tree, err := scan.Scan(context.Background(), root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return tree, vars
}

func byRule(fs []Finding) map[string][]Finding {
	m := map[string][]Finding{}
	for _, f := range fs {
		m[f.Rule.ID] = append(m[f.Rule.ID], f)
	}
	return m
}

func mustPack(t *testing.T) *Pack {
	t.Helper()
	p, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Proves: path rules with variables and "*" segments find caches,
// matching folder names case-insensitively.
func TestMatchPathRules(t *testing.T) {
	testutil.RequireWindows(t)
	tree, vars := fixture(t, map[string]int{
		"home/go/pkg/mod/github.com/x/y.go":                                 50_000,
		"home/AppData/Local/go-build/aa/bb":                                 40_000,
		"home/AppData/Local/Google/Chrome/User Data/Default/Cache/data_1":   30_000,
		"home/AppData/Local/Google/Chrome/User Data/Profile 2/cache/data_2": 20_000,
		"home/AppData/Local/Google/Chrome/User Data/Default/History":        10_000,
	})
	got := byRule(Match(tree, mustPack(t), vars, nil))

	if f := got["go-mod-cache"]; len(f) != 1 || f[0].Alloc < 50_000 {
		t.Errorf("go-mod-cache = %+v", f)
	}
	if f := got["go-build-cache"]; len(f) != 1 || f[0].Owner != "Go" {
		t.Errorf("go-build-cache = %+v", f)
	}
	if f := got["chrome-cache"]; len(f) != 2 {
		t.Errorf("chrome-cache found %d profiles, want 2 (Default and lowercase 'cache' in Profile 2)", len(f))
	}
}

// Proves: name rules need their sibling marker, and nested matches inside
// an already matched folder are not reported again.
func TestMatchNameRules(t *testing.T) {
	testutil.RequireWindows(t)
	tree, vars := fixture(t, map[string]int{
		"code/web/package.json":                           100,
		"code/web/node_modules/a/index.js":                30_000,
		"code/web/node_modules/a/node_modules/b/index.js": 20_000,
		"code/notjs/node_modules/x.txt":                   10_000, // no package.json
		"code/rusty/Cargo.toml":                           100,
		"code/rusty/target/debug/app.exe":                 40_000,
		"code/other/target/readme.txt":                    10_000, // no Cargo.toml
		"code/app/App.csproj":                             100,
		"code/app/obj/project.assets.json":                15_000,
	})
	got := byRule(Match(tree, mustPack(t), vars, nil))

	nm := got["node-modules"]
	if len(nm) != 1 || !strings.HasSuffix(nm[0].Node.Path(), filepath.Join("web", "node_modules")) {
		t.Fatalf("node-modules = %+v, want only code/web/node_modules", nm)
	}
	if nm[0].Alloc < 50_000 {
		t.Errorf("node-modules alloc = %d, want nested package included (>= 50000)", nm[0].Alloc)
	}
	if rt := got["rust-target"]; len(rt) != 1 {
		t.Errorf("rust-target = %d findings, want 1", len(rt))
	}
	if d := got["dotnet-obj"]; len(d) != 1 {
		t.Errorf("dotnet-obj = %d findings, want 1 (glob sibling *.csproj)", len(d))
	}
}

// Proves: name rules never mark folders inside programs, app data or other
// findings as "safe to clean". A node_modules inside VS Code's install
// folder is part of the program; deleting it would break VS Code.
func TestNameRulesSkipProgramAndAppAreas(t *testing.T) {
	testutil.RequireWindows(t)
	tree, vars := fixture(t, map[string]int{
		// inside an installed program (outside the app areas, e.g. D:\Tools)
		"tools/Editor/package.json":            100,
		"tools/Editor/node_modules/x/index.js": 30_000,
		// inside a path-rule finding (npm cache)
		"home/AppData/Local/npm-cache/_npx/1/package.json":   100,
		"home/AppData/Local/npm-cache/_npx/1/node_modules/y": 30_000,
		// an unregistered tool's runtime in AppData
		"home/AppData/Local/agent/app/package.json":   100,
		"home/AppData/Local/agent/app/node_modules/z": 30_000,
		// inside Program Files
		"Program Files/Thing/package.json":   100,
		"Program Files/Thing/node_modules/w": 30_000,
		// an editor extension in a dot-folder of the home directory
		"home/.vscode/extensions/ext-1.0/package.json":   100,
		"home/.vscode/extensions/ext-1.0/node_modules/u": 30_000,
		// a project inside a dot-folder deeper down is still a project
		"home/code/.hidden-proj/package.json":   100,
		"home/code/.hidden-proj/node_modules/t": 30_000,
		// a real project: must still be found
		"home/code/site/package.json":   100,
		"home/code/site/node_modules/v": 30_000,
	})
	apps := []registry.App{{Name: "Editor", InstallDir: filepath.Join(tree.Path(), "tools", "Editor")}}
	got := byRule(Match(tree, mustPack(t), vars, apps))

	var paths []string
	for _, f := range got["node-modules"] {
		rel, _ := filepath.Rel(tree.Path(), f.Node.Path())
		paths = append(paths, filepath.ToSlash(rel))
	}
	sort.Strings(paths)
	want := []string{"home/code/.hidden-proj/node_modules", "home/code/site/node_modules"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("node-modules matched %v, want only the projects %v", paths, want)
	}
}

// Proves: nested findings are listed separately and subtracted from the
// outer finding, so the totals never count a byte twice.
func TestMatchSubtractsNestedFindings(t *testing.T) {
	testutil.RequireWindows(t)
	tree, vars := fixture(t, map[string]int{
		"home/AppData/Local/Packages/SomeApp/LocalState/db":              100_000,
		"home/AppData/Local/Packages/MSTeams_8wekyb3d8bbwe/LocalCache/c": 300_000,
	})
	got := byRule(Match(tree, mustPack(t), vars, nil))

	outer, inner := got["store-app-data"], got["teams-cache"]
	if len(outer) != 1 || len(inner) != 1 {
		t.Fatalf("store-app-data=%d teams-cache=%d findings, want 1 each", len(outer), len(inner))
	}
	whole := outer[0].Node.Alloc
	if outer[0].Alloc+inner[0].Alloc != whole {
		t.Errorf("outer %d + inner %d != folder %d", outer[0].Alloc, inner[0].Alloc, whole)
	}
	if outer[0].Alloc >= inner[0].Alloc {
		t.Errorf("outer exclusive %d should be smaller than Teams cache %d", outer[0].Alloc, inner[0].Alloc)
	}
}

// Proves: installed programs claim their install folder, and their data
// folders in AppData are found by app or publisher name, but not for
// Microsoft and not when the name is too short to be meaningful.
func TestMatchInstalledApps(t *testing.T) {
	testutil.RequireWindows(t)
	tree, vars := fixture(t, map[string]int{
		"Program Files/Foo Editor/foo.exe":        80_000,
		"home/AppData/Roaming/FooEditor/settings": 20_000,
		"home/AppData/Local/JetSoft/cache.bin":    30_000,
		"home/AppData/Local/Microsoft/huge.bin":   90_000,
		"home/AppData/Local/Abc/x.bin":            10_000,
	})
	root := tree.Path()
	apps := []registry.App{
		{Name: "Foo Editor", Publisher: "Microsoft Corporation", InstallDir: filepath.Join(root, "Program Files", "Foo Editor")},
		{Name: "Bar IDE", Publisher: "JetSoft s.r.o."},
		{Name: "Abc", Publisher: "Abc"},
	}
	got := byRule(Match(tree, mustPack(t), vars, apps))

	if f := got["installed-app"]; len(f) != 1 || f[0].Owner != "Foo Editor" || f[0].App == nil || f[0].App.Name != "Foo Editor" {
		t.Errorf("installed-app = %+v", f)
	}
	owners := map[string]bool{}
	for _, f := range got["app-data"] {
		owners[f.Owner+"@"+f.Node.Name] = true
	}
	for _, want := range []string{"Foo Editor@FooEditor", "JetSoft@JetSoft"} {
		if !owners[want] {
			t.Errorf("missing app-data %s; got %v", want, owners)
		}
	}
	for k := range owners {
		if strings.HasSuffix(k, "@Microsoft") || strings.HasSuffix(k, "@Abc") {
			t.Errorf("unexpected app-data %s", k)
		}
	}
}

// Proves: a rule wins over the installed-app heuristic on the same folder,
// and an empty readable folder produces no finding.
func TestMatchPrecedenceAndEmpty(t *testing.T) {
	testutil.RequireWindows(t)
	tree, vars := fixture(t, map[string]int{
		"home/AppData/Local/npm-cache/_cacache/x": 10_000,
	})
	if err := os.MkdirAll(filepath.Join(vars["LOCALAPPDATA"], "Temp"), 0o755); err != nil {
		t.Fatal(err)
	}
	apps := []registry.App{{Name: "npm cache", InstallDir: vars["NPM_CACHE"]}}
	got := byRule(Match(tree, mustPack(t), vars, apps))

	if len(got["npm-cache"]) != 1 || len(got["installed-app"]) != 0 {
		t.Errorf("npm-cache=%d installed-app=%d, want rule to win", len(got["npm-cache"]), len(got["installed-app"]))
	}
	if len(got["temp-user"]) != 0 {
		t.Error("empty Temp folder should not be a finding")
	}
}

// Proves: rules referencing variables that are unknown on this machine are
// skipped instead of matching a wrong relative path.
func TestExpandFailsOnMissingVariable(t *testing.T) {
	testutil.RequireWindows(t)
	if _, ok := expand(`{GOCACHE}\x`, locations.Vars{}); ok {
		t.Error("expand succeeded without GOCACHE")
	}
	if p, ok := expand(`{SYSTEMDRIVE}\hiberfil.sys`, locations.Vars{"SYSTEMDRIVE": `C:\`}); !ok || p != `C:\hiberfil.sys` {
		t.Errorf("expand = %q, %v", p, ok)
	}
}

func TestTrimCompanySuffix(t *testing.T) {
	for in, want := range map[string]string{
		"JetBrains s.r.o.":      "JetBrains",
		"Microsoft Corporation": "Microsoft",
		"Valve Corp.":           "Valve",
		"Foo, Inc.":             "Foo",
		"Docker Inc":            "Docker",
		"Plain":                 "Plain",
	} {
		if got := trimCompanySuffix(in); got != want {
			t.Errorf("trimCompanySuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
