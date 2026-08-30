package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha/oai"
)

// ErrWindowOpen and ErrNoWindow report a writer used out of order.
var (
	ErrWindowOpen = errors.New("store: a window is already open")
	ErrNoWindow   = errors.New("store: no window is open")
)

// Writer appends harvested responses to one group of one shard.
//
// The unit of atomicity is the window, as it was in v1, where a window became
// real when its files were renamed into place. Here it becomes real when one
// transaction records the segment's new committed length together with the
// window and its records. Bytes appended before a crash are still in the
// segment file, past that length, and the next open drops them.
//
// A writer holds the shard's lock for its lifetime, so a second harvest of the
// same endpoint - any group of it - fails fast instead of interleaving.
type Writer struct {
	baseDir string
	id      Identity
	shard   string
	group   Group

	lock *os.File
	st   *state
	g    *groupState
	meta *Meta
	enc  *zstd.Encoder

	seg          *segWriter
	segNum       int
	segCommitted int64 // the length the index vouches for

	win *openWindow
}

// openWindow accumulates what a window will commit. What it does not accumulate
// is where each record went: a window's bytes are one run in one segment, from
// the length the index vouched for when it opened to the length it vouches for
// when it commits.
type openWindow struct {
	from, until time.Time
	settled     bool
	started     time.Time
	requests    int
	bytes       int64
	records     int
	deleted     int
	lo, hi      string // the datestamp bracket over every record so far
}

// cover grows the window's datestamp bracket to hold a response's. A response
// that carried no records says nothing about datestamps.
func (o *openWindow) cover(sc scanned) {
	switch {
	case sc.Records == 0:
	case o.records == sc.Records: // the first response with anything in it
		o.lo, o.hi = sc.Lo, sc.Hi
	default:
		o.lo, o.hi = coverStamps(o.lo, o.hi, sc.Lo, sc.Hi)
	}
}

// OpenWriter opens a shard for appending, creating it if necessary, and takes
// the shard lock. Callers must Close.
//
// An identity still in the pre-1.0 layout is refused, so that a harvest does not
// quietly start a second copy in a shard beside data it cannot see - which would
// refetch the endpoint whole and leave the operator with two half caches.
func OpenWriter(baseDir string, id Identity) (*Writer, error) {
	if id.BaseURL == "" {
		return nil, ErrNoBaseURL
	}
	if err := refuseLegacy(baseDir, id); err != nil {
		return nil, err
	}
	return openWriter(baseDir, id)
}

// openWriter is OpenWriter without the refusal, for Migrate: the whole point
// there is that the source is in the pre-1.0 layout and the shard is not written
// yet.
func openWriter(baseDir string, id Identity) (w *Writer, err error) {
	shard := shardDir(baseDir, id.BaseURL)
	if err := os.MkdirAll(shard, 0755); err != nil {
		return nil, err
	}
	lock, err := TryFlock(filepath.Join(shard, LockName))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && lock != nil {
			lock.Close()
		}
	}()
	meta, err := readMeta(shard)
	if errors.Is(err, os.ErrNotExist) {
		meta = &Meta{Layout: layoutName, BaseURL: id.BaseURL, Created: time.Now().UTC()}
	} else if err != nil {
		return nil, err
	}
	group := id.group()
	if !meta.hasGroup(group) {
		meta.Groups = append(meta.Groups, group)
	}
	if err := writeMeta(shard, meta); err != nil {
		return nil, err
	}
	st, err := loadState(filepath.Join(shard, stateName))
	if err != nil {
		return nil, err
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	w = &Writer{
		baseDir: baseDir,
		id:      id,
		shard:   shard,
		group:   group,
		lock:    lock,
		st:      st,
		g:       st.ensureGroup(group.Format, group.Set),
		meta:    meta,
		enc:     enc,
	}
	if err := w.openSegment(); err != nil {
		return nil, err
	}
	return w, nil
}

// openSegment picks up the group's newest segment, or starts the first one,
// and truncates whatever a previous crash left past the committed length.
//
// The new segment is only recorded in memory: the next commit writes the index
// out whole, and a harvest that dies before then leaves a file the next open
// truncates back to nothing. So a shard whose first harvest got nowhere costs no
// index at all.
func (w *Writer) openSegment() error {
	var (
		num       = 1
		committed int64
	)
	if n := len(w.g.Segments); n > 0 {
		last := w.g.Segments[n-1]
		if last.Committed >= segMaxSize {
			num = last.Num + 1 // Rotate; the old segment stays as it is.
		} else {
			num, committed = last.Num, last.Committed
		}
	}
	if w.g.segment(num) == nil {
		w.g.Segments = append(w.g.Segments, &segRow{Num: num})
	}
	path := filepath.Join(w.shard, segDirname, w.group.Dir, segFileName(num))
	seg, err := openSegWriter(path, committed, w.enc)
	if err != nil {
		return err
	}
	w.seg, w.segNum, w.segCommitted = seg, num, committed
	return nil
}

// Close drops any open window and releases the shard lock.
func (w *Writer) Close() error {
	var errs []error
	if w.win != nil {
		errs = append(errs, w.Abort(nil))
	}
	if w.seg != nil {
		errs = append(errs, w.seg.close())
	}
	if w.enc != nil {
		w.enc.Close()
	}
	if w.lock != nil {
		errs = append(errs, w.lock.Close())
	}
	return errors.Join(errs...)
}

// Identity returns what this writer harvests into.
func (w *Writer) Identity() Identity { return w.id }

// Dir returns the shard directory.
func (w *Writer) Dir() string { return w.shard }

// SetIdentify records the endpoint's identify response, which is what makes a
// shard self-describing: granularity and earliest datestamp are the two things
// a harvest needs and v1 had nowhere to keep.
func (w *Writer) SetIdentify(identify *oai.Identify) error {
	w.meta.Identify = identify
	return writeMeta(w.shard, w.meta)
}

// Identify returns the recorded identify response, if there is one.
func (w *Writer) Identify() *oai.Identify { return w.meta.Identify }

// Resume returns the instant a harvest continues from, or the zero time if
// this group holds nothing yet. It is the v2 answer to a readdir over dated
// filenames, and a more exact one: a window that failed, or that could only
// report what existed at the moment it was fetched, hands back its own start,
// so the range is covered again rather than resumed past.
// Into local time: boundaries are the edges of local days, and in most of the
// world such an instant belongs to a different UTC date, so a caller that goes
// on to think in days would be off by one.
func (w *Writer) Resume() time.Time {
	if unsettled := w.g.unsettledFrom(); !unsettled.IsZero() {
		return unsettled.In(time.Local)
	}
	if last := w.g.lastWindow(); !last.IsZero() {
		return nextAfter(last).In(time.Local)
	}
	return time.Time{}
}

// HasWindow reports whether a range has already been harvested, so that a
// re-run can skip it - including a window that yielded no records at all,
// which in v1 had to be remembered by writing an otherwise pointless file.
func (w *Writer) HasWindow(from, until time.Time) bool {
	return w.g.hasWindow(from.UTC(), until.UTC())
}

// WindowRecords returns how many records the shard holds for one range, which
// is what checking an already migrated window against its source needs.
func (w *Writer) WindowRecords(from, until time.Time) int {
	return w.g.windowRecords(from.UTC(), until.UTC())
}

// Begin opens a window. Every Append until Commit belongs to it. Pass settled
// false when the range reaches into a stretch of time the endpoint can still
// add records to, so that Resume comes back to it.
func (w *Writer) Begin(from, until time.Time, settled bool) error {
	if w.win != nil {
		return ErrWindowOpen
	}
	w.win = &openWindow{from: from, until: until, settled: settled, started: time.Now().UTC()}
	return nil
}

// Append adds one raw response to the open window. The bytes go to the segment
// as they are; what is parsed out of them is only the count and the datestamp
// bracket the index keeps.
func (w *Writer) Append(raw []byte) error {
	if w.win == nil {
		return ErrNoWindow
	}
	sc, err := scanResponse(raw)
	if err != nil {
		return fmt.Errorf("scan response: %w", err)
	}
	w.seg.append(raw)
	w.win.records += sc.Records
	w.win.deleted += sc.Deleted
	w.win.cover(sc)
	w.win.requests++
	w.win.bytes += int64(len(raw))
	if w.seg.pending() >= frameTarget {
		return w.seg.flush()
	}
	return nil
}

// Commit makes the open window durable: the frames reach the disk first, then
// the index is written out whole and renamed into place, recording how far the
// segment is good for, what the window cost, and the run of bytes it appended.
func (w *Writer) Commit() error {
	if w.win == nil {
		return ErrNoWindow
	}
	if err := w.seg.flush(); err != nil {
		return err
	}
	if err := w.seg.sync(); err != nil {
		return err
	}
	// Unsettled beats empty: a window that reached into the present has to be
	// fetched again whether or not anything was there at the time.
	status := statusOK
	switch {
	case !w.win.settled:
		status = statusPartial
	case w.win.records == 0:
		status = statusEmpty
	}
	row := w.win.row(status)
	// Everything appended since the window opened, which is a whole number of
	// frames: nothing else writes into a segment while a window is open, and a
	// segment rotates only between windows.
	if n := w.seg.size - w.segCommitted; n > 0 {
		row.Extents = []extent{{Seg: w.segNum, Off: w.segCommitted, Len: n}}
	}
	if err := w.st.commitWindow(w.g, row, w.segNum, w.seg.size); err != nil {
		return err
	}
	w.segCommitted = w.seg.size
	w.win = nil
	if w.seg.size >= segMaxSize {
		// Rotate between windows only, so a window is never split across
		// files and recovery only ever has one torn segment to consider.
		if err := w.seg.close(); err != nil {
			return err
		}
		return w.openSegment()
	}
	return nil
}

// Abort drops the open window: the bytes it appended are cut back off the
// segment, and if a reason is given it is recorded, so a later run can tell an
// unharvested range from one that failed.
func (w *Writer) Abort(cause error) error {
	if w.win == nil {
		return ErrNoWindow
	}
	win := w.win
	w.win = nil
	if err := w.seg.truncate(w.segCommitted); err != nil {
		return err
	}
	if cause == nil {
		return nil
	}
	row := win.row(statusError)
	// A failed window claims no records, whatever it had appended before it gave
	// up: those bytes have just been cut back off the segment. What it keeps is
	// what it cost - the requests and the bytes fetched are spent either way.
	row.Records, row.Deleted, row.Lo, row.Hi = 0, 0, "", ""
	row.Err = cause.Error()
	// The segment is back at the length the index already vouches for, so this
	// records it unchanged rather than as a special case.
	return w.st.commitWindow(w.g, row, w.segNum, w.segCommitted)
}

// row renders what a window accumulated as the index row for it.
func (o *openWindow) row(status string) windowRow {
	row := windowRow{
		From:     o.from.UTC(),
		Until:    o.until.UTC(),
		Status:   status,
		Requests: o.requests,
		Records:  o.records,
		Deleted:  o.deleted,
		Bytes:    o.bytes,
		Started:  o.started,
		Finished: time.Now().UTC(),
		Lo:       o.lo,
		Hi:       o.hi,
	}
	row.Elapsed = elapsed(row.Started, row.Finished)
	return row
}

// elapsed is how long a window took, which is stored rather than derived so
// that it survives being merged: two runs that share a row are still two spells
// of work with idle time between them, and the wall clock across both is not
// what "time spent harvesting" means.
func elapsed(started, finished time.Time) int64 {
	d := finished.Sub(started)
	if started.IsZero() || finished.IsZero() || d < 0 {
		return 0
	}
	return int64(d)
}

// removeV2 forgets one group of a shard: its segments and its rows. The shard
// stays, since other formats and sets of the same endpoint live in it.
func removeV2(baseDir string, id Identity) error {
	shard := shardDir(baseDir, id.BaseURL)
	if !isShard(shard) {
		return nil
	}
	lock, err := TryFlock(filepath.Join(shard, LockName))
	if err != nil {
		return err
	}
	if lock != nil {
		defer lock.Close()
	}
	st, err := loadState(filepath.Join(shard, stateName))
	if err != nil {
		return err
	}
	if g := st.group(id.Format, id.Set); g != nil {
		if err := st.dropGroup(g); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(shard, segDirname, groupName(id.Format, id.Set))); err != nil {
		return err
	}
	meta, err := readMeta(shard)
	if err != nil {
		return err
	}
	meta.removeGroup(id.group())
	return writeMeta(shard, meta)
}

// SegmentBytes returns how many bytes this group's segments occupy. Superseded
// copies count: they are on disk, which is what a caller asking about size
// wants to know.
func (w *Writer) SegmentBytes() int64 { return w.g.segmentBytes() }

// Records returns how many records the open window has seen so far.
func (w *Writer) Records() int {
	if w.win == nil {
		return 0
	}
	return w.win.records
}
