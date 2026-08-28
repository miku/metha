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
//
// windows is a map of what is covered, not a log of what was run: settled rows
// that touch are merged, so one row can stand for years and any number of
// invocations. Its counters are therefore sums over the range - elapsed_ns
// among them, which is kept as its own total because started and finished mean
// first reached and last touched, and the span between them stopped being time
// spent harvesting the moment two runs shared a row.
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
	id         INTEGER PRIMARY KEY,
	group_id   INTEGER NOT NULL REFERENCES groups(id),
	from_ts    TEXT NOT NULL,
	until_ts   TEXT NOT NULL,
	status     TEXT NOT NULL,
	requests   INTEGER NOT NULL DEFAULT 0,
	records    INTEGER NOT NULL DEFAULT 0,
	bytes      INTEGER NOT NULL DEFAULT 0,
	elapsed_ns INTEGER NOT NULL DEFAULT 0,
	started    TEXT,
	finished   TEXT,
	err        TEXT,
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
`

// Window statuses. An empty window is a first class outcome: it costs a row
// and no bytes, where v1 had to write a file to remember it at all.
const (
	statusOK    = "ok"
	statusEmpty = "empty"
	statusError = "error"
	// statusPartial marks a window that was fetched but is not final: it
	// reaches into a stretch of time the endpoint can still add records to, so
	// what came back is only what existed at the moment of asking. It is the
	// row a harvest resumes at rather than past.
	statusPartial = "partial"
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
	if err := prepareSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &state{db: db}, nil
}

// schemaVersion is the shape of the index this code writes. It is stamped into
// the file, so a shard and the binary opening it can disagree out loud instead
// of quietly. Raise it whenever schema changes, and add the step that gets a
// shard there to migrations.
const schemaVersion = 1

// applicationID marks the file as a metha index: "MTHA", big endian. sqlite
// keeps it in the header and never reads it, so it costs nothing and lets both
// file(1) and metha tell a shard from some other database that happens to be
// sitting at the same path.
const applicationID = 0x4D544841

// migrations upgrades a shard a version at a time: migrations[v] takes it from
// v to v+1. There is no entry for 0, because a shard from before the version
// stamp existed cannot be recognised - the same zero covers every shape the
// index had while it was being designed - and guessing at one is what the stamp
// is here to stop.
var migrations = map[int][]string{}

// errUnversioned marks a shard written before the index carried a version.
// Only metha 0.5 development builds made those, and their segments were written
// under boundary rules that have since changed, so re-harvesting is the honest
// answer rather than reading them as if nothing had moved.
var errUnversioned = errors.New("index predates the version stamp: remove the shard directory and harvest again")

// prepareSchema brings a shard's index to the version this code writes, and
// refuses anything it cannot account for. A fresh file gets the schema and the
// stamp; a known older one is walked up through migrations; a newer one is left
// alone, because a binary that does not know the shape of a file is in no
// position to write into it.
func prepareSchema(db *sql.DB) error {
	return prepareSchemaTo(db, schemaVersion, migrations)
}

// prepareSchemaTo is prepareSchema with the target version and the steps to
// reach it passed in, so that the ladder can be walked in a test. There is no
// other reason for the seam: nothing but prepareSchema calls it in anger.
func prepareSchemaTo(db *sql.DB, target int, steps map[int][]string) error {
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		if _, err := db.Exec(schema); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
		return stamp(db, target)
	}
	var appID, version int
	if err := db.QueryRow(`SELECT * FROM pragma_application_id`).Scan(&appID); err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT * FROM pragma_user_version`).Scan(&version); err != nil {
		return err
	}
	switch {
	case version == 0 || appID != applicationID:
		return errUnversioned
	case version > target:
		return fmt.Errorf("index is version %d, this metha writes %d: upgrade metha", version, target)
	}
	for v := version; v < target; v++ {
		up, ok := steps[v]
		if !ok {
			return fmt.Errorf("no way to upgrade an index from version %d to %d", v, v+1)
		}
		// One transaction per step, together with the stamp, so an upgrade that
		// is interrupted leaves the shard at a version that describes it.
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		for _, step := range up {
			if _, err := tx.Exec(step); err != nil {
				tx.Rollback()
				return fmt.Errorf("upgrading index from version %d: %w", v, err)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	// Tables the running code added since this shard was made. Every statement
	// in schema is IF NOT EXISTS, so this only ever creates what is missing -
	// which is the whole of what it can do, and why it is not a substitute for
	// the migrations above.
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	return nil
}

// stamp writes the two header integers that say what this file is and which
// shape it has. Neither takes a page of its own; they live in the header sqlite
// writes anyway.
func stamp(db *sql.DB, version int) error {
	for _, q := range []string{
		fmt.Sprintf(`PRAGMA application_id = %d`, applicationID),
		fmt.Sprintf(`PRAGMA user_version = %d`, version),
	} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
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

// segmentBytes returns how many bytes of a group's segments the index vouches
// for. That is the committed length rather than the file size, so the torn tail
// of a crashed harvest, which the next open truncates, is not counted.
func (s *state) segmentBytes(groupID int64) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRow(`SELECT SUM(committed_size) FROM segments WHERE group_id = ?`, groupID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n.Int64, err
}

// lastWindow returns the largest window end of a group, or the empty string if
// nothing was harvested into it yet. This is the resume point.
//
// The boundless window of a -no-intervals harvest stores the empty string,
// which sorts below every real boundary and so loses the maximum to any of
// them. A group that holds nothing else answers the empty string, and a harvest
// that switches to intervals starts from the endpoint's earliest date - the
// only honest answer, since an unbounded fetch says nothing about what it
// covered.
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

// unsettledFrom returns the start of the earliest window that is not final -
// one that failed, or one that covered a range the endpoint could still add to
// - or the empty string when every window is settled. It is the resume point
// whenever there is one, ahead of lastWindow: resuming past a window that only
// holds what existed at the time of asking is how updates go missing.
//
// This is also what retries a window that failed in the middle of a range,
// which a high water mark alone can only do for the newest one.
//
// A window with no boundaries - the one a -no-intervals harvest writes, which
// covers whatever the endpoint chose to send - is skipped. It has no start to
// resume from, and it makes no claim about which ranges are covered, so it must
// not answer a question about them either.
func (s *state) unsettledFrom(groupID int64) (string, error) {
	var from sql.NullString
	err := s.db.QueryRow(`SELECT MIN(from_ts) FROM windows WHERE group_id = ? AND status IN (?, ?) AND from_ts <> ''`,
		groupID, statusPartial, statusError).Scan(&from)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return from.String, nil
}

// hasWindow reports whether a range was already harvested successfully, which
// is what makes a re-run skip work instead of refetching it.
//
// A range is covered when some settled window contains it, not when one matches
// it exactly: settled windows are merged as they are committed, so the row that
// answers for a range is usually wider than the range, and after a year of
// daily harvests it is a great deal wider.
func (s *state) hasWindow(groupID int64, from, until string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM windows
		WHERE group_id = ? AND from_ts <= ? AND until_ts >= ? AND status NOT IN (?, ?)`,
		groupID, from, until, statusError, statusPartial).Scan(&n)
	return n > 0, err
}

// windowRecords returns how many records the index holds for the windows lying
// inside a range. A migration compares this against its source, so the answer
// comes from the records themselves rather than from the count a window row was
// stamped with.
//
// Windows inside the range, rather than one matching it: merging means a
// migrated cache answers out of a single row spanning everything it holds, and
// asking about one day of it would find nothing. A window reaching past the
// range is left out, so a shard that has been harvested further reports short
// rather than counting records the range never claimed.
func (s *state) windowRecords(groupID int64, from, until string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM records
		JOIN windows ON records.window_id = windows.id
		WHERE windows.group_id = ? AND windows.from_ts >= ? AND windows.until_ts <= ?`,
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

// elapsed returns how long the window took, which is stored rather than derived
// so that it survives being merged: two runs that share a row are still two
// spells of work with idle time between them, and the wall clock across both is
// not what "time spent harvesting" means.
func (w windowRow) elapsed() int64 {
	d := w.Finished.Sub(w.Started)
	if w.Started.IsZero() || w.Finished.IsZero() || d < 0 {
		return 0
	}
	return int64(d)
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
	// An unsettled window starting inside the range this one covers has just
	// been fetched again, under whatever boundaries this run chose. Usually
	// that is the same range and the upsert below is all it takes; it is not
	// when the interval size changed between runs, and then the stale row would
	// hold the resume point back forever. Settled windows are left alone: they
	// are answers that still stand.
	for _, q := range []string{
		`DELETE FROM records WHERE window_id IN (
			SELECT id FROM windows WHERE group_id = ? AND status IN (?, ?) AND from_ts >= ? AND from_ts <= ?)`,
		`DELETE FROM windows WHERE group_id = ? AND status IN (?, ?) AND from_ts >= ? AND from_ts <= ?`,
	} {
		if _, err := tx.Exec(q, win.GroupID, statusPartial, statusError, win.From, win.Until); err != nil {
			return err
		}
	}
	// A settled window that begins where a settled window ends is one stretch
	// of time fetched in two goes, and the index is never asked how many goes
	// it took - only which ranges are covered. Folding the new one into its
	// neighbour is what keeps this table a map of coverage rather than a log of
	// invocations: a group harvested daily for a decade holds one row instead of
	// three and a half thousand, and one that is polled every minute holds one
	// instead of half a million.
	//
	// What it costs is the per-window detail. Requests, bytes and duration
	// become sums over the merged range, and the range itself stops recording
	// where one run stopped and the next began.
	windowID, err := extendSettled(tx, win)
	if err != nil {
		return err
	}
	if windowID == 0 {
		if _, err := tx.Exec(`
			INSERT INTO windows (group_id, from_ts, until_ts, status, requests, records, bytes, elapsed_ns, started, finished, err)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(group_id, from_ts, until_ts) DO UPDATE SET
				status = excluded.status, requests = excluded.requests, records = excluded.records,
				bytes = excluded.bytes, elapsed_ns = excluded.elapsed_ns,
				started = excluded.started, finished = excluded.finished,
				err = excluded.err`,
			win.GroupID, win.From, win.Until, win.Status, win.Requests, win.Records, win.Bytes,
			win.elapsed(), ts(win.Started), ts(win.Finished), win.Err); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT id FROM windows WHERE group_id = ? AND from_ts = ? AND until_ts = ?`,
			win.GroupID, win.From, win.Until).Scan(&windowID); err != nil {
			return err
		}
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

// settled reports whether a status describes a range that is final, and so one
// that may be folded into its neighbour. A window that failed or that only
// holds what existed at the moment of asking has to keep its own boundaries,
// because those boundaries are where the next run resumes.
func settled(status string) bool { return status == statusOK || status == statusEmpty }

// extendSettled folds a settled window into the settled window that ends where
// it begins, and returns that row's id. It returns zero when there is nothing
// to fold into, which is the caller's cue to insert a row of its own.
//
// The window keeps the started of the earlier half, since that is when the
// merged range was first reached for, and takes the finished of the later one.
// It is empty only while nothing has been found anywhere in it.
func extendSettled(tx *sql.Tx, win windowRow) (int64, error) {
	// A window with no boundaries covers whatever the endpoint chose to send
	// and abuts nothing; see unsettledFrom.
	if !settled(win.Status) || win.From == "" {
		return 0, nil
	}
	from, err := parseWindowTime(win.From)
	if err != nil {
		return 0, err
	}
	var id int64
	var records int
	err = tx.QueryRow(`SELECT id, records FROM windows
		WHERE group_id = ? AND until_ts = ? AND status IN (?, ?)`,
		win.GroupID, ts(from.Add(-time.Nanosecond)), statusOK, statusEmpty).Scan(&id, &records)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	status := statusOK
	if records+win.Records == 0 {
		status = statusEmpty
	}
	_, err = tx.Exec(`UPDATE windows SET
		until_ts = ?, status = ?, requests = requests + ?, records = records + ?,
		bytes = bytes + ?, elapsed_ns = elapsed_ns + ?, finished = ? WHERE id = ?`,
		win.Until, status, win.Requests, win.Records, win.Bytes, win.elapsed(), ts(win.Finished), id)
	return id, err
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

// windowTime is how an instant is stored. Every field is fixed width, the zone
// is always UTC and so always the same letter, and the nanoseconds are always
// spelled out.
//
// That is what makes text order the same as time order, which the index relies
// on everywhere: MIN(from_ts), MAX(until_ts), the overlap delete in
// commitWindow, the equality in hasWindow. RFC3339Nano, the obvious choice,
// trims trailing zeros - and "." sorts below "Z", so it puts 12:00:00.5 before
// 12:00:00, which is backwards. Writing the zeros costs nine bytes a row and
// leaves nothing for a call site to get right.
const windowTime = "2006-01-02T15:04:05.000000000Z"

// ts renders a timestamp for storage, or the empty string for the zero time,
// which is how a window with no boundaries at all is spelled - see
// unsettledFrom.
func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(windowTime)
}

// parseWindowTime reads a stored boundary back. Into local time: boundaries are
// the edges of local days, and in most of the world such an instant belongs to
// a different UTC date, so a caller that goes on to think in days would be off
// by one.
func parseWindowTime(stored string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, stored)
	if err != nil {
		return time.Time{}, fmt.Errorf("window boundary %q: %w", stored, err)
	}
	return t.In(time.Local), nil
}

// windowDate renders a stored boundary as a date, which is what a listing shows
// for how far an endpoint got.
func windowDate(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	t, err := parseWindowTime(stored)
	if err != nil {
		return "", err
	}
	return t.Format("2006-01-02"), nil
}

// nextAfter returns where a harvest picks up after a settled window ended at t.
// Both OAI bounds are inclusive, so the next window starts just past this one,
// or the boundary would be fetched twice. Every boundary a window is recorded
// with is exact - a date-only bound is stored as the end of the day it stands
// for - so a nanosecond is all it takes.
func nextAfter(t time.Time) time.Time {
	return t.Add(time.Nanosecond)
}
