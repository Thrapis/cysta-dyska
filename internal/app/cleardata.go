package app

import "errors"

// ClearData deletes what the app generated itself: the scan history, the
// action log and leftover Full scan result files. Settings are kept. Files
// outside the app's own data are never touched. It refuses while a scan or an
// action is running.
func (a *App) ClearData() error {
	if !a.actionMu.TryLock() {
		return errors.New("another action is still running")
	}
	defer a.actionMu.Unlock()

	// Held for the whole clearing, so no scan can start in the middle of it.
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return errors.New("wait for the scan to finish")
	}

	if a.history != nil {
		if err := a.history.ClearHistory(); err != nil {
			return err
		}
	}
	a.snapshotID, a.historyWarning = 0, ""
	if a.goos == "windows" {
		return a.clearFastResults()
	}
	return nil
}
