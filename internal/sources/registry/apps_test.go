package registry

import (
	"runtime"
	"testing"

	"cysta-dyska/internal/testutil"
)

// Proves: install folders are cleaned up from the messy forms found in the
// wild (quotes, trailing slashes, icon paths with ",0" suffixes), and
// folders that would wrongly claim huge shared areas are rejected.
func TestInstallDir(t *testing.T) {
	testutil.RequireWindows(t)
	cases := []struct {
		name, location, icon, want string
	}{
		{"plain", `C:\Program Files\Foo`, "", `C:\Program Files\Foo`},
		{"quotes and slash", `"C:\Program Files\Foo\"`, "", `C:\Program Files\Foo`},
		{"icon fallback", "", `"C:\Program Files\Bar\bar.exe",0`, `C:\Program Files\Bar`},
		{"location wins over icon", `D:\Apps\Baz`, `C:\Other\x.exe`, `D:\Apps\Baz`},
		{"drive root rejected", `C:\`, "", ""},
		{"program files itself rejected", `C:\Program Files`, "", ""},
		{"windows icon rejected", "", `C:\Windows\System32\shell32.dll,5`, ""},
		{"installer cache icon rejected", "", `C:\Windows\Installer\{GUID}\icon.ico`, ""},
		{"relative rejected", `Foo\Bar`, "", ""},
		{"user profile rejected", `C:\Users\me`, "", ""},
		{"local appdata rejected", `C:\Users\me\AppData\Local`, "", ""},
		{"local programs rejected", `C:\Users\me\AppData\Local\Programs`, "", ""},
		{"app under local programs ok", `C:\Users\me\AppData\Local\Programs\Thing`, "", `C:\Users\me\AppData\Local\Programs\Thing`},
	}
	for _, c := range cases {
		if got := installDir(c.location, c.icon); got != c.want {
			t.Errorf("%s: installDir(%q, %q) = %q, want %q", c.name, c.location, c.icon, got, c.want)
		}
	}
}

// Proves: the DisplayIcon forms found in the wild (quotes, ",index" suffix,
// negative resource ids, bare paths, junk) give the right file and icon index.
func TestParseIcon(t *testing.T) {
	cases := []struct {
		in, path string
		index    int
	}{
		{`C:\Foo\foo.exe`, `C:\Foo\foo.exe`, 0},
		{`"C:\Foo\foo.exe",0`, `C:\Foo\foo.exe`, 0},
		{`C:\Foo\foo.exe,3`, `C:\Foo\foo.exe`, 3},
		{` "C:\Program Files\Foo\foo.exe" , -101 `, `C:\Program Files\Foo\foo.exe`, -101},
		{`"C:\Foo, Inc\foo.ico"`, `C:\Foo, Inc\foo.ico`, 0},
		{`C:\Foo, Inc\foo.ico`, `C:\Foo, Inc\foo.ico`, 0},
		{`C:\Foo\foo.exe,notanumber`, `C:\Foo\foo.exe,notanumber`, 0},
		{"", "", 0},
		{`""`, "", 0},
	}
	for _, c := range cases {
		path, index := parseIcon(c.in)
		if path != c.path || index != c.index {
			t.Errorf("parseIcon(%q) = %q, %d; want %q, %d", c.in, path, index, c.path, c.index)
		}
	}
}

// Proves: reading the real registry works on this machine, finds named
// apps, and never returns duplicates of the same app.
func TestInstalledReadsRealRegistry(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	apps, err := Installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) == 0 {
		t.Fatal("no installed apps found")
	}
	seen := map[string]bool{}
	for _, a := range apps {
		if a.Name == "" {
			t.Errorf("app without name: %+v", a)
		}
		key := a.Name + "|" + a.Version + "|" + a.InstallDir
		if seen[key] {
			t.Errorf("duplicate app: %+v", a)
		}
		seen[key] = true
	}
}
