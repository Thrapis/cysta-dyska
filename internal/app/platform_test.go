package app

import (
	"testing"

	"cysta-dyska/internal/attribute"
)

// Proves: on macOS the app is explain-only. Planning and running actions and
// the full (MFT) scan are refused before anything is looked at, and findings
// offer no actions. Removing windowsOnly from any of them makes this fail.
func TestMacOSIsExplainOnly(t *testing.T) {
	a, _ := newTestApp(t.TempDir(), nil)
	a.goos = "darwin"

	if _, err := a.PlanAction(ActionRequest{RuleID: "x", Path: "/x", Kind: KindRecycle}); err != errWindowsOnly {
		t.Errorf("PlanAction err = %v, want errWindowsOnly", err)
	}
	if _, err := a.RunAction("id", true); err != errWindowsOnly {
		t.Errorf("RunAction err = %v, want errWindowsOnly", err)
	}
	if err := a.StartFastScan(`C:\`); err != errWindowsOnly {
		t.Errorf("StartFastScan err = %v, want errWindowsOnly", err)
	}
	if got := a.Platform(); got != "darwin" {
		t.Errorf("Platform = %q", got)
	}

	// A finding whose rule opts in to file actions still gets none.
	a.mu.Lock()
	a.culprits = &CulpritsDTO{Items: []CulpritDTO{{}}}
	a.findings = []attribute.Finding{{Rule: &attribute.Rule{
		Risk: attribute.RiskSafe, FileAction: attribute.FileActionFolder,
	}}}
	a.mu.Unlock()
	res, err := a.Culprits()
	if err != nil {
		t.Fatal(err)
	}
	if res.Items[0].Actions == nil || len(res.Items[0].Actions) != 0 {
		t.Errorf("Actions = %#v, want empty non-nil slice", res.Items[0].Actions)
	}
}
