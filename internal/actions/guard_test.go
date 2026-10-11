package actions

import (
	"testing"

	"cysta-dyska/internal/sources/locations"
)

func testVars() locations.Vars {
	return locations.Vars{
		"USERPROFILE":     `C:\Users\me`,
		"LOCALAPPDATA":    `C:\Users\me\AppData\Local`,
		"APPDATA":         `C:\Users\me\AppData\Roaming`,
		"TEMP":            `C:\Users\me\AppData\Local\Temp`,
		"PROGRAMDATA":     `C:\ProgramData`,
		"PROGRAMFILES":    `C:\Program Files`,
		"PROGRAMFILESX86": `C:\Program Files (x86)`,
		"SYSTEMROOT":      `C:\Windows`,
		"SYSTEMDRIVE":     `C:\`,
	}
}

// Proves: file actions are refused for every place where removing files
// would hurt Windows, programs, other users or whole user folders, and
// allowed for ordinary caches and project folders.
func TestGuard(t *testing.T) {
	g := NewGuard(testVars())
	refused := []string{
		``, `relative\path`, `C:\`, `E:\`, `E:\Codebase`,
		`C:\Windows`, `C:\Windows\Temp`, `C:\Windows\System32\drivers`,
		`C:\Program Files`, `C:\Program Files\App\cache`, `C:\Program Files (x86)\Steam\steamapps`,
		`C:\ProgramData`, `C:\Users`, `C:\Users\other\AppData\Local\npm-cache`, `C:\Users\Public\Documents`,
		`C:\Users\me`, `C:\Users\me\AppData`, `C:\Users\me\AppData\Local`, `C:\Users\me\AppData\Roaming`,
		`C:\Users\me\Documents`, `C:\Users\me\Desktop`, `C:\Users\me\Downloads`, `C:\Users\me\OneDrive`,
		`C:\$Recycle.Bin\S-1-5-21`, `C:\System Volume Information\x`, `D:\$RECYCLE.BIN`,
		`C:\Users\me\..\other\x`, `\\server\share\x`, `\\?\C:\Users\me\x`,
	}
	for _, p := range refused {
		if err := g.Check(p); err == nil {
			t.Errorf("Check(%q) allowed, want refused", p)
		}
	}
	allowed := []string{
		`C:\Users\me\AppData\Local\npm-cache`, `C:\Users\me\AppData\Local\Temp\x.tmp`,
		`C:\Users\me\go\pkg\mod`, `c:\users\ME\appdata\local\uv\cache`,
		`C:\Users\me\code\site\node_modules`, `E:\Codebase\web\node_modules`,
		`C:\ProgramData\NVIDIA Corporation\Downloader`, `C:\Users\me\Documents\proj\target`,
	}
	for _, p := range allowed {
		if err := g.Check(p); err != nil {
			t.Errorf("Check(%q) refused: %v", p, err)
		}
	}
}

// Proves: with base folders missing from the environment, the guard still
// refuses system paths instead of failing open.
func TestGuardWithoutVars(t *testing.T) {
	g := NewGuard(locations.Vars{})
	for _, p := range []string{`C:\`, `C:\Windows\Temp`, `C:\Program Files\x`, `C:\Users\me`} {
		if err := g.Check(p); err == nil {
			t.Errorf("Check(%q) allowed with no vars", p)
		}
	}
}
