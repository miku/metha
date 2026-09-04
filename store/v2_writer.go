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
// A writer holds the group's lock for its lifetime, so a second harvest of the
// same format and set of the same endpoint fails fast instead of interleaving.
// Another format of the same endpoint is another directory holding another index
// and other segments, and runs alongside it.
type Writer struct {
	baseDir string
	id      Identity
	shard   string
	group   Group

	lock *os.File
	st   *state
	meta *Meta
	enc  *zstd.Encoder

	// announced records whether meta.json has been written for this run. It is
	// what makes the shard visible to a listing, and it waits for the first
	// commit; see announce.
	announced bool

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
	group := id.group()
	if err := os.MkdirAll(groupDir(shard, group.Format, group.Set), 0755); err != nil {
		return nil, err
	}
	// The group's lock, not the shard's: two formats of one endpoint write
	// different segments and different indexes, and there was never a reason for
	// one to wait on the other. It is held for the writer's lifetime, so a second
	// harvest of the same group still fails fast instead of interleaving.
	lock, err := TryFlock(lockPath(shard, group.Format, group.Set))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && lock != nil {
			_ = lock.Close()
		}
	}()
	meta, err := readMeta(shard)
	if errors.Is(err, os.ErrNotExist) {
		meta = &Meta{Layout: layoutName, BaseURL: id.BaseURL, Created: time.Now().UTC()}
	} else if err != nil {
		return nil, err
	}
	st, err := loadState(statePath(shard, group.Format, group.Set), group.Format, group.Set)
	if err != nil {
		return nil, err
	}
	// One encoder goroutine, explicitly. zstd.NewWriter otherwise sizes its
	// internal concurrency at GOMAXPROCS and allocates window and hash state
	// per goroutine, so the cost of an idle encoder is a function of the
	// machine rather than of the work: measured at 1.55MB per writer at
	// concurrency 1, and 80.8MB at 64.
	//
	// That is paid per open Writer, and a sweep opens one per endpoint being
	// harvested. At --jobs 256 on a 64-core machine it came to 20GB of
	// encoders holding nothing, which is what a sweep on a 128GB machine was
	// seen spending half its memory on.
	//
	// Nothing is lost. Encoder concurrency parallelises the blocks of a single
	// stream, and every stream here is written serially by the one goroutine
	// harvesting that endpoint; the parallelism that matters is already at the
	// endpoint level, where the sweep put it.
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	// The segment file and meta.json are not opened or written here. Nothing a
	// listing can see exists until a window commits, so a harvest that got
	// nowhere - a URL that was not an endpoint, a plan that could not be made,
	// a first request that failed - leaves no shard for metha stat to report as
	// zero of everything. See Close, which takes the directory back too.
	w = &Writer{
		baseDir: baseDir,
		id:      id,
		shard:   shard,
		group:   group,
		lock:    lock,
		st:      st,
		meta:    meta,
		enc:     enc,
	}
	if err := w.dropTornTail(); err != nil {
		return nil, err
	}
	return w, nil
}

// dropTornTail cuts the newest segment back to the length the index vouches for,
// which is the whole of crash recovery: bytes past it are what a harvest that
// died mid-window appended, named by no extent and reachable by no read.
//
// It happens on open, as it always has, but by truncating a file rather than by
// opening one - opening the segment is now the first append's job, and a writer
// that appends nothing must leave no file behind. A segment that does not exist
// has no tail, so this is a stat and usually nothing else.
func (w *Writer) dropTornTail() error {
	if len(w.st.Segments) == 0 {
		return nil
	}
	last := w.st.Segments[len(w.st.Segments)-1]
	path := filepath.Join(groupDir(w.shard, w.group.Format, w.group.Set), segFileName(last.Num))
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() <= last.Committed {
		return nil
	}
	return os.Truncate(path, last.Committed)
}

// updateMeta applies a change to the shard's meta and writes it back, creating
// it if this is the first harvest of the endpoint. It returns the meta as
// written, whether or not fn changed anything.
//
// meta.json is the one file the groups of an endpoint share, and every change to
// it is a read, a change and a write. Two harvests of different formats starting
// at the same moment would otherwise each read the group list without the
// other's entry and each write it back without it, and a group missing from that
// list is data a listing does not mention and a read reports as unharvested.
// Which is why this, and only this, happens under the shard lock: it is held
// across one small read-modify-write and nothing else, and always after the
// group lock, so the two can never wait on each other.
func updateMeta(shard string, id Identity, fn func(*Meta) bool) (*Meta, error) {
	unlock, err := lockShard(shard)
	if err != nil {
		return nil, err
	}
	defer unlock()
	meta, err := readMeta(shard)
	if errors.Is(err, os.ErrNotExist) {
		meta = &Meta{Layout: layoutName, BaseURL: id.BaseURL, Created: time.Now().UTC()}
	} else if err != nil {
		return nil, err
	}
	if !fn(meta) {
		return meta, nil
	}
	if err := writeMeta(shard, meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// announce writes the shard's meta: the endpoint, what it said about itself,
// and this group among the ones the shard holds. It is what makes the shard
// visible to a listing, so it happens on the first commit rather than on open -
// the first moment there is something to list.
//
// Once per writer: the group list and the identify only change when a run
// starts, so a harvest of a thousand windows writes meta.json once, as it did
// when this happened at open.
func (w *Writer) announce() error {
	if w.announced {
		return nil
	}
	group, identify := w.group, w.meta.Identify
	meta, err := updateMeta(w.shard, w.id, func(m *Meta) bool {
		if !m.hasGroup(group) {
			m.Groups = append(m.Groups, group)
		}
		// The identify this run was given, if it was given one. Held in memory
		// until now for the same reason as everything else here: a harvest that
		// commits nothing should not leave a shard describing an endpoint it
		// never read a record from.
		if identify != nil {
			m.Identify = identify
		}
		return true
	})
	if err != nil {
		return err
	}
	w.meta, w.announced = meta, true
	return nil
}

// openSegment picks up the group's newest segment, or starts the first one,
// and truncates whatever a previous crash left past the committed length.
//
// It runs on the first append rather than on open, so that a writer nothing was
// ever written through creates no file. The new segment is only recorded in
// memory: the next commit writes the index out whole, and a harvest that dies
// before then leaves a file the next open truncates back to nothing. So a shard
// whose first harvest got nowhere costs no index at all.
func (w *Writer) openSegment() error {
	var (
		num       = 1
		committed int64
	)
	if n := len(w.st.Segments); n > 0 {
		last := w.st.Segments[n-1]
		if last.Committed >= segMaxSize {
			num = last.Num + 1 // Rotate; the old segment stays as it is.
		} else {
			num, committed = last.Num, last.Committed
		}
	}
	if w.st.segment(num) == nil {
		w.st.Segments = append(w.st.Segments, &segRow{Num: num})
	}
	path := filepath.Join(groupDir(w.shard, w.group.Format, w.group.Set), segFileName(num))
	seg, err := openSegWriter(path, committed, w.enc)
	if err != nil {
		return err
	}
	w.seg, w.segNum, w.segCommitted = seg, num, committed
	return nil
}

// Close drops any open window, releases the group lock, and takes back the
// directories if this writer never wrote anything into them.
func (w *Writer) Close() error {
	var errs []error
	if w.win != nil {
		errs = append(errs, w.Abort(nil))
	}
	if w.seg != nil {
		errs = append(errs, w.seg.close())
		w.seg.discardIfEmpty()
	}
	if w.enc != nil {
		errs = append(errs, w.enc.Close())
	}
	if w.lock != nil {
		errs = append(errs, w.lock.Close())
	}
	discardEmpty(w.baseDir, w.shard, groupDir(w.shard, w.group.Format, w.group.Set))
	return errors.Join(errs...)
}

// discardEmpty removes the directories a writer had to create to hold its lock,
// when nothing was ever written into them - a harvest that could not plan, or
// whose first request failed. Without this an endpoint that has never yielded a
// record still leaves a directory behind, and over a sweep of a quarter million
// URLs that is a quarter million of them.
//
// It removes only what is empty, which is the whole of the safety argument:
// os.Remove refuses a directory with anything in it, so a group that holds an
// index or segments, or a shard that holds meta.json or another group, stops the
// walk on its own. No flag says whether this run created them, and none is
// needed.
//
// Unlinking the lock file while still holding the lock is safe here because the
// work is over. A second harvest that opens the path between the unlink and the
// removal gets a new file and a lock on it, which is correct: this writer is not
// going to write anything either way. What that costs is one ENOTEMPTY on the
// next line, ignored along with every other error - failing to tidy up is not a
// reason to fail a harvest that otherwise worked.
func discardEmpty(baseDir, shard, group string) {
	entries, err := os.ReadDir(group)
	if err != nil || len(entries) != 1 || entries[0].Name() != LockName {
		return
	}
	if err := os.Remove(filepath.Join(group, LockName)); err != nil {
		return
	}
	// Up through the group, the shard and its two fan-out levels, stopping at
	// the first directory that still holds something - or at the cache root,
	// which is the user's and not ours to remove.
	for dir := group; dir != baseDir && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			return
		}
	}
}

// Identity returns what this writer harvests into.
func (w *Writer) Identity() Identity { return w.id }

// Dir returns the shard directory.
func (w *Writer) Dir() string { return w.shard }

// SetIdentify records the endpoint's identify response, which is what makes a
// shard self-describing: granularity and earliest datestamp are the two things
// a harvest needs and v1 had nowhere to keep.
//
// Recorded in memory and written by the first commit, along with everything
// else that makes the shard visible. A harvest that never commits a window has
// nothing to describe, and a shard describing an endpoint no record was ever
// read from is exactly the empty row this laziness exists to prevent.
//
// The write, when it comes, goes through updateMeta rather than putting back the
// copy this writer opened with - that would be a lost update with teeth, since a
// harvest of another format that started later is in the group list on disk and
// not in that copy, so writing it back would drop a group that has segments.
func (w *Writer) SetIdentify(identify *oai.Identify) error {
	w.meta.Identify = identify
	return nil
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
	if unsettled := w.st.unsettledFrom(); !unsettled.IsZero() {
		return unsettled.In(time.Local)
	}
	if last := w.st.lastWindow(); !last.IsZero() {
		return nextAfter(last).In(time.Local)
	}
	return time.Time{}
}

// HasWindow reports whether a range has already been harvested, so that a
// re-run can skip it - including a window that yielded no records at all,
// which in v1 had to be remembered by writing an otherwise pointless file.
func (w *Writer) HasWindow(from, until time.Time) bool {
	return w.st.hasWindow(from.UTC(), until.UTC())
}

// WindowRecords returns how many records the shard holds for one range, which
// is what checking an already migrated window against its source needs.
func (w *Writer) WindowRecords(from, until time.Time) int {
	return w.st.windowRecords(from.UTC(), until.UTC())
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
	// The first response of the writer's life is what brings the segment into
	// existence. Scanning first is deliberate: a response that cannot be read is
	// not worth creating a file for.
	if w.seg == nil {
		if err := w.openSegment(); err != nil {
			return err
		}
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
	// This is the moment the shard becomes a thing that exists: a window is
	// about to be recorded, so there is something for a listing to report.
	// Before it, nothing has been written at all.
	if err := w.announce(); err != nil {
		return err
	}
	// A window that appended nothing has no segment to flush - an empty range
	// costs a row and no bytes, and the first response of the harvest is what
	// opens the file.
	if w.seg != nil {
		if err := w.seg.flush(); err != nil {
			return err
		}
		if err := w.seg.sync(); err != nil {
			return err
		}
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
	if w.seg == nil {
		if err := w.st.commitWindow(row, 0, 0); err != nil {
			return err
		}
		w.win = nil
		return nil
	}
	// Everything appended since the window opened, which is a whole number of
	// frames: nothing else writes into a segment while a window is open, and a
	// segment rotates only between windows.
	if n := w.seg.size - w.segCommitted; n > 0 {
		row.Extents = []extent{{Seg: w.segNum, Off: w.segCommitted, Len: n}}
	}
	if err := w.st.commitWindow(row, w.segNum, w.seg.size); err != nil {
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
	// Nothing was appended, so there is nothing to cut back off.
	if w.seg != nil {
		if err := w.seg.truncate(w.segCommitted); err != nil {
			return err
		}
	}
	if cause == nil {
		return nil
	}
	// A failure is a thing worth recording, so it makes the shard visible the
	// same way a commit does - a run that reached an endpoint and was refused
	// has learned something, where one that never got a plan together has not.
	if err := w.announce(); err != nil {
		return err
	}
	row := win.row(statusError)
	// A failed window claims no records, whatever it had appended before it gave
	// up: those bytes have just been cut back off the segment. What it keeps is
	// what it cost - the requests and the bytes fetched are spent either way.
	row.Records, row.Deleted, row.Lo, row.Hi = 0, 0, "", ""
	row.Err = cause.Error()
	// The segment is back at the length the index already vouches for, so this
	// records it unchanged rather than as a special case.
	return w.st.commitWindow(row, w.segNum, w.segCommitted)
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

// removeV2 forgets one group of a shard: its segments, its index and its lock,
// which are now one directory. The shard stays, since other formats and sets of
// the same endpoint live beside it.
func removeV2(baseDir string, id Identity) error {
	shard := shardDir(baseDir, id.BaseURL)
	if !isShard(shard) {
		return nil
	}
	dir := groupDir(shard, id.Format, id.Set)
	// Taken and released rather than held: the lock file is inside what is about
	// to be deleted, so holding it across the removal would mean holding a lock
	// on a file that no longer exists. Taking it first is still what keeps this
	// from running while a harvest of the same group does.
	lock, err := TryFlock(lockPath(shard, id.Format, id.Set))
	if errors.Is(err, os.ErrNotExist) {
		// Nothing of this group is on disk, so there is nothing to lock and
		// nothing to remove but the meta entry.
	} else if err != nil {
		return err
	} else if lock != nil {
		_ = lock.Close()
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	_, err = updateMeta(shard, id, func(m *Meta) bool {
		m.removeGroup(id.group())
		return true
	})
	return err
}

// SegmentBytes returns how many bytes this group's segments occupy. Superseded
// copies count: they are on disk, which is what a caller asking about size
// wants to know.
func (w *Writer) SegmentBytes() int64 { return w.st.segmentBytes() }

// Records returns how many records the open window has seen so far.
func (w *Writer) Records() int {
	if w.win == nil {
		return 0
	}
	return w.win.records
}
