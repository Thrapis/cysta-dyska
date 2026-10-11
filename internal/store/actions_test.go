package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// Proves: a database created by the M4 version (schema 1) is upgraded in
// place: its snapshots survive and the new tables work.
func TestMigrateFromVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO snapshots (root, root_key, taken_at, alloc, size, files, dirs, duration_ms, min_alloc, max_depth)
		VALUES ('C:\x', 'c:\x', 1, 2, 3, 4, 5, 6, 7, 8); PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.Snapshots(`C:\x`)
	if err != nil || len(list) != 1 {
		t.Fatalf("snapshots after upgrade = %v, %v", list, err)
	}
	if err := s.SetSetting("k", "v"); err != nil {
		t.Fatalf("settings table missing after upgrade: %v", err)
	}
	var v int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
}

// Proves: settings round-trip and missing keys are reported as absent.
func TestSettings(t *testing.T) {
	s := openTest(t)
	if _, ok, err := s.Setting("allow_delete"); ok || err != nil {
		t.Fatalf("missing setting: ok=%v err=%v", ok, err)
	}
	if err := s.SetSetting("allow_delete", "true"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("allow_delete", "false"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Setting("allow_delete"); !ok || err != nil || v != "false" {
		t.Errorf("setting = %q, %v, %v", v, ok, err)
	}
}

// Proves: an action is logged as running before it starts and updated when
// it ends, so the log shows actions interrupted by a crash; the log lists
// newest first.
func TestActionLog(t *testing.T) {
	s := openTest(t)
	id, err := s.StartAction(ActionRecord{
		Kind: "recycle", RuleID: "npm-cache", Owner: "npm", Target: `C:\npm`, BytesBefore: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	crashed, err := s.StartAction(ActionRecord{Kind: "command", Command: "go clean -cache"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAction(id, "done", "", 0); err != nil {
		t.Fatal(err)
	}

	log, err := s.Actions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].ID != crashed || log[1].ID != id {
		t.Fatalf("log = %+v", log)
	}
	if log[0].Outcome != "running" {
		t.Errorf("unfinished action outcome = %q, want running", log[0].Outcome)
	}
	if log[1].Outcome != "done" || log[1].BytesBefore != 1000 || log[1].BytesAfter != 0 || log[1].At.IsZero() {
		t.Errorf("finished action = %+v", log[1])
	}
	if log[1].At.After(time.Now().Add(time.Hour * 24 * 365 * 10)) {
		t.Error("timestamp is nonsense")
	}
}
