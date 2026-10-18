package app

import (
	"context"
	"testing"

	"cysta-dyska/internal/store"
)

func storeAction() store.ActionRecord {
	return store.ActionRecord{Kind: "recycle", Owner: "npm", Target: `C:\npm`}
}

// Proves: ClearData empties the history and action log, forgets the current
// snapshot, keeps settings, and clears Full scan leftovers on Windows.
func TestClearData(t *testing.T) {
	a, em := newHistoryApp(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]int{"cache/blob.bin": 100_000})
	scanAndWait(t, a, em, root)
	if err := a.SetAllowPermanentDelete(true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.history.StartAction(storeAction()); err != nil {
		t.Fatal(err)
	}
	cleared := 0
	a.goos = "windows"
	a.clearFastResults = func() error { cleared++; return nil }

	if err := a.ClearData(); err != nil {
		t.Fatal(err)
	}

	h, err := a.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Snapshots) != 0 || h.CurrentID != 0 {
		t.Errorf("history after clearing = %+v", h)
	}
	if log, _ := a.ActionLog(); len(log) != 0 {
		t.Errorf("action log after clearing = %+v", log)
	}
	if !a.Settings().AllowPermanentDelete {
		t.Error("settings were cleared; they must be kept")
	}
	if cleared != 1 {
		t.Errorf("fast scan results cleared %d times, want 1", cleared)
	}
}

// Proves: ClearData does not touch Full scan files off Windows, where that
// feature does not exist.
func TestClearDataSkipsFastScanOffWindows(t *testing.T) {
	a, _ := newHistoryApp(t)
	a.goos = "darwin"
	a.clearFastResults = func() error { t.Error("fast scan folder touched off Windows"); return nil }
	if err := a.ClearData(); err != nil {
		t.Fatal(err)
	}
}

// Proves: ClearData refuses while a scan is running and while an action
// runs, and deletes nothing then.
func TestClearDataRefusedWhileBusy(t *testing.T) {
	a, _ := newHistoryApp(t)
	if _, err := a.history.StartAction(storeAction()); err != nil {
		t.Fatal(err)
	}
	a.clearFastResults = func() error { t.Error("cleared while busy"); return nil }

	_, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	if err := a.ClearData(); err == nil {
		t.Error("cleared during a scan")
	}
	a.cancel = nil

	a.actionMu.Lock()
	if err := a.ClearData(); err == nil {
		t.Error("cleared during an action")
	}
	a.actionMu.Unlock()

	if log, _ := a.ActionLog(); len(log) != 1 {
		t.Errorf("action log has %d entries, want 1: nothing may be deleted when refused", len(log))
	}
}
