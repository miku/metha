package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // pure go, so CGO_ENABLED=0 keeps working
)

// schema is the per-shard index. It is derived data: every row can be rebuilt
// by rereading the segments, which is what keeps "metha is a cache" true and
// recovery down to a rebuild.
//
// The tables that matter for correctness are segments and windows. A segment's
// committed_size is the only statement about which bytes of a segment file are
// real; anything past it is the tail of a harvest that died mid-window and is
// truncated on the next open. A window row is what says a range has been
// fetched, which in v1 was the mere existence of a file - and which is why a
// window with no records needed a file there and needs none here.
const schema = `
CREATE TABLE IF NOT EXISTS groups (
	id      INTEGER PRIMARY KEY,
	format  TEXT NOT NULL,
	setspec TEXT NOT NULL,
	dir     TEXT NOT NULL,
	UNIQUE(format, setspec)
);

CREATE TABLE IF NOT EXISTS segments (
	id             INTEGER PRIMARY KEY,
	group_id       INTEGER NOT NULL REFERENCES groups(id),
	name           TEXT NOT NULL,
	committed_size INTEGER NOT NULL DEFAULT 0,
	UNIQUE(group_id, name)
);

CREATE TABLE IF NOT EXISTS windows (
	id       INTEGER PRIMARY KEY,
	group_id INTEGER NOT NULL REFERENCES groups(id),
	from_ts  TEXT NOT NULL,
	until_ts TEXT NOT NULL,
	status   TEXT NOT NULL,
	requests INTEGER NOT NULL DEFAULT 0,
	records  INTEGER NOT NULL DEFAULT 0,
	bytes    INTEGER NOT NULL DEFAULT 0,
	started  TEXT,
	finished TEXT,
	err      TEXT,
	UNIQUE(group_id, from_ts, until_ts)
);

CREATE TABLE IF NOT EXISTS records (
	id         INTEGER PRIMARY KEY,
	window_id  INTEGER NOT NULL REFERENCES windows(id),
	seg        INTEGER NOT NULL REFERENCES segments(id),
	identifier TEXT NOT NULL,
	datestamp  TEXT NOT NULL,
	status     TEXT NOT NULL,
	setspec    TEXT NOT NULL,
	frame_off  INTEGER NOT NULL,
	frame_len  INTEGER NOT NULL,
	rec_off    INTEGER NOT NULL,
	rec_len    INTEGER NOT NULL,
	sha256     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS records_datestamp ON records(datestamp);
CREATE INDEX IF NOT EXISTS records_identifier ON records(identifier);

CREATE TABLE IF NOT EXISTS runs (
	id       INTEGER PRIMARY KEY,
	started  TEXT NOT NULL,
	finished TEXT,
	requests INTEGER NOT NULL DEFAULT 0,
	bytes    INTEGER NOT NULL DEFAULT 0,
	records  INTEGER NOT NULL DEFAULT 0,
	errors   TEXT
);
`

// Window statuses. An empty window is a first class outcome: it costs a row
// and no bytes, where v1 had to write a file to remember it at all.
const (
	statusOK    = "ok"
	statusEmpty = "empty"
	statusError = "error"
)

// state is a shard's sqlite index.
type state struct {
	db *sql.DB
}

// openState opens, and if necessary creates, the index of one shard. WAL keeps
// a reader from blocking the harvest that is writing; the single connection
// keeps concurrent statements inside one process from fighting over the write
// lock, which the per-shard flock already serialises between processes.
func openState(path string) (*state, error) {
	dsn := url.URL{
		Scheme: "file",
		Path:   path,
		RawQuery: url.Values{"_pragma": []string{
			"busy_timeout(10000)",
			"journal_mode(WAL)",
			"synchronous(NORMAL)",
			"foreign_keys(1)",
		}}.Encode(),
	}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &state{db: db}, nil
}

func (s *state) close() error { return s.db.Close() }

// ensureGroup returns the id of a group, creating the row if this is the first
// time this format and set are harvested into the shard.
func (s *state) ensureGroup(g Group) (int64, error) {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO groups (format, setspec, dir) VALUES (?, ?, ?)`,
		g.Format, g.Set, g.Dir); err != nil {
		return 0, err
	}
	return s.groupID(g.Format, g.Set)
}

// groupID returns the id of a group, or zero if the shard has none.
func (s *state) groupID(format, set string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM groups WHERE format = ? AND setspec = ?`, format, set).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// segRow is a segment file and the prefix of it that is committed.
type segRow struct {
	ID        int64
	Name      string
	Committed int64
}

// segments lists a group's segments in write order.
func (s *state) segments(groupID int64) ([]segRow, error) {
	rows, err := s.db.Query(`SELECT id, name, committed_size FROM segments WHERE group_id = ? ORDER BY id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var segs []segRow
	for rows.Next() {
		var seg segRow
		if err := rows.Scan(&seg.ID, &seg.Name, &seg.Committed); err != nil {
			return nil, err
		}
		segs = append(segs, seg)
	}
	return segs, rows.Err()
}

// newSegment records a new, empty segment file.
func (s *state) newSegment(groupID int64, name string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO segments (group_id, name, committed_size) VALUES (?, ?, 0)`, groupID, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// lastWindow returns the largest window end of a group, or the empty string if
// nothing was harvested into it yet. This is the resume point.
func (s *state) lastWindow(groupID int64) (string, error) {
	var until sql.NullString
	err := s.db.QueryRow(`SELECT MAX(until_ts) FROM windows WHERE group_id = ? AND status != ?`,
		groupID, statusError).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return until.String, nil
}

// hasWindow reports whether a range was already harvested successfully, which
// is what makes a re-run skip work instead of refetching it.
func (s *state) hasWindow(groupID int64, from, until string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM windows WHERE group_id = ? AND from_ts = ? AND until_ts = ? AND status != ?`,
		groupID, from, until, statusError).Scan(&n)
	return n > 0, err
}

// windowRecords returns how many records the index holds for one range. A
// migration checking a window it did not write this run compares this against
// the source, so the answer comes from the records themselves rather than from
// the count the window row was stamped with.
func (s *state) windowRecords(groupID int64, from, until string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM records
		JOIN windows ON records.window_id = windows.id
		WHERE windows.group_id = ? AND windows.from_ts = ? AND windows.until_ts = ?`,
		groupID, from, until).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// windowRow is one harvested range, with what it cost and what it yielded.
type windowRow struct {
	GroupID  int64
	From     string
	Until    string
	Status   string
	Requests int
	Records  int
	Bytes    int64
	Started  time.Time
	Finished time.Time
	Err      string
}

// recordRow locates one record inside a segment, and carries the header fields
// worth filtering on without decompressing anything.
type recordRow struct {
	Seg        int64
	Identifier string
	Datestamp  string
	Status     string
	SetSpec    string
	FrameOff   int64
	FrameLen   int64
	RecOff     int64
	RecLen     int64
	Sum        string
}

// commitWindow is the transaction that makes a harvested window real: the
// segment's committed length, the window row and the record rows land together
// or not at all. Bytes appended past the committed length are the torn tail of
// a crash and are dropped on the next open.
func (s *state) commitWindow(win windowRow, segID, segSize int64, recs []recordRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if segID != 0 {
		if _, err := tx.Exec(`UPDATE segments SET committed_size = ? WHERE id = ?`, segSize, segID); err != nil {
			return err
		}
	}
	// A window can be harvested again, after an error or a forced refetch; its
	// old rows go, the bytes in the segment stay, since the blob layer is
	// append-only and dedupe is a reader's decision.
	if _, err := tx.Exec(`
		DELETE FROM records WHERE window_id IN (
			SELECT id FROM windows WHERE group_id = ? AND from_ts = ? AND until_ts = ?)`,
		win.GroupID, win.From, win.Until); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO windows (group_id, from_ts, until_ts, status, requests, records, bytes, started, finished, err)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(group_id, from_ts, until_ts) DO UPDATE SET
			status = excluded.status, requests = excluded.requests, records = excluded.records,
			bytes = excluded.bytes, started = excluded.started, finished = excluded.finished,
			err = excluded.err`,
		win.GroupID, win.From, win.Until, win.Status, win.Requests, win.Records, win.Bytes,
		ts(win.Started), ts(win.Finished), win.Err); err != nil {
		return err
	}
	var windowID int64
	if err := tx.QueryRow(`SELECT id FROM windows WHERE group_id = ? AND from_ts = ? AND until_ts = ?`,
		win.GroupID, win.From, win.Until).Scan(&windowID); err != nil {
		return err
	}
	if len(recs) > 0 {
		stmt, err := tx.Prepare(`
			INSERT INTO records (window_id, seg, identifier, datestamp, status, setspec,
				frame_off, frame_len, rec_off, rec_len, sha256)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, rec := range recs {
			if _, err := stmt.Exec(windowID, rec.Seg, rec.Identifier, rec.Datestamp, rec.Status,
				rec.SetSpec, rec.FrameOff, rec.FrameLen, rec.RecOff, rec.RecLen, rec.Sum); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// dropGroup forgets a group entirely: its records, its windows, its segments
// and the group itself. The segment files are the caller's to delete.
func (s *state) dropGroup(groupID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM records WHERE window_id IN (SELECT id FROM windows WHERE group_id = ?)`,
		`DELETE FROM windows WHERE group_id = ?`,
		`DELETE FROM segments WHERE group_id = ?`,
		`DELETE FROM groups WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, groupID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// countRecords returns the number of indexed records of a group, which is what
// a migration compares against its source.
func (s *state) countRecords(groupID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM records
		JOIN windows ON records.window_id = windows.id
		WHERE windows.group_id = ?`, groupID).Scan(&n)
	return n, err
}

// ts renders a timestamp for storage, or the empty string for the zero time.
// Boundaries are stored as UTC so that they sort lexically, which is what makes
// MAX(until_ts) the resume point.
func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	// Nanosecond precision, so that a window that took a fraction of a second
	// still reports a duration and a rate.
	return t.UTC().Format(time.RFC3339Nano)
}

// windowDate renders a stored boundary as the date a harvest resumes from.
// Back into local time first: a window boundary is the end of a local day, and
// in most of the world that instant belongs to a different UTC date, so
// formatting the stored value directly would move the resume point by a day.
func windowDate(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, stored)
	if err != nil {
		return "", fmt.Errorf("window boundary %q: %w", stored, err)
	}
	return t.In(time.Local).Format("2006-01-02"), nil
}
