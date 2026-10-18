package store

import (
	"database/sql"
	"errors"
	"time"
)

// ActionRecord is one entry of the cleanup log.
type ActionRecord struct {
	ID          int64
	At          time.Time
	Kind        string // command, admin, recycle, delete, uninstall
	RuleID      string
	Owner       string
	Target      string // the finding's path
	Command     string // what was run, as shown to the user
	BytesBefore int64
	BytesAfter  int64
	Outcome     string // running, done, failed, declined, started, interrupted
	Detail      string // error or the end of the command's output
}

// StartAction logs an action as running, before it starts, so an action
// interrupted by a crash still shows up in the log.
func (s *Store) StartAction(r ActionRecord) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO actions
		(at, kind, rule_id, owner, target, command, bytes_before, bytes_after, outcome, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, 'running', '')`,
		s.now().UTC().UnixMilli(), r.Kind, r.RuleID, r.Owner, r.Target, r.Command, r.BytesBefore)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishAction records how an action ended.
func (s *Store) FinishAction(id int64, outcome, detail string, bytesAfter int64) error {
	res, err := s.db.Exec(`UPDATE actions SET outcome = ?, detail = ?, bytes_after = ? WHERE id = ?`,
		outcome, detail, bytesAfter, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("action not found")
	}
	return nil
}

// MarkInterrupted marks actions still logged as running as interrupted. Call
// it once at startup, when this process holds the single-instance lock: a
// running entry then means the previous run ended in the middle of an
// action, so it may have finished only part of the work.
func (s *Store) MarkInterrupted() (int64, error) {
	res, err := s.db.Exec(`UPDATE actions SET outcome = 'interrupted',
		detail = 'The app closed before this finished. Some of it may have been done; scan again to see.'
		WHERE outcome = 'running'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Actions returns the newest log entries first.
func (s *Store) Actions(limit int) ([]ActionRecord, error) {
	rows, err := s.db.Query(`SELECT id, at, kind, rule_id, owner, target, command,
		bytes_before, bytes_after, outcome, detail FROM actions ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActionRecord
	for rows.Next() {
		var r ActionRecord
		var at int64
		if err := rows.Scan(&r.ID, &at, &r.Kind, &r.RuleID, &r.Owner, &r.Target, &r.Command,
			&r.BytesBefore, &r.BytesAfter, &r.Outcome, &r.Detail); err != nil {
			return nil, err
		}
		r.At = time.UnixMilli(at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClearHistory deletes all snapshots (their entries go with them) and the
// whole action log. Settings are kept. The database stays open and in use, so
// rows are deleted instead of the file.
func (s *Store) ClearHistory() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM actions`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM snapshots`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Give the space back; the clearing itself has already succeeded.
	s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	s.db.Exec(`VACUUM`)
	return nil
}

// Setting returns a stored setting; ok is false if it was never set.
func (s *Store) Setting(key string) (value string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// SetSetting stores a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
