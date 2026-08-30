package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha"
)

// Writer is what a harvest writes through when it targets this layout.
var _ metha.Sink = (*Writer)(nil)

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
	groupID int64

	lock *os.File
	st   *state
	meta *Meta
	enc  *zstd.Encoder

	seg          *segWriter
	segID        int64
	segNum       int
	segCommitted int64 // the length the index vouches for

	win *openWindow
}

// openWindow accumulates what a window will commit.
type openWindow struct {
	from, until time.Time
	settled     bool
	started     time.Time
	requests    int
	bytes       int64
	records     []recordRow
	// unflushed is the index into records from which rows are still waiting
	// for the frame they will land in.
	unflushed int
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
	lock, err := metha.TryFlock(filepath.Join(shard, metha.LockName))
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
	st, err := openState(filepath.Join(shard, stateName))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			st.close()
		}
	}()
	groupID, err := st.ensureGroup(group)
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
		groupID: groupID,
		lock:    lock,
		st:      st,
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
func (w *Writer) openSegment() error {
	segs, err := w.st.segments(w.groupID)
	if err != nil {
		return err
	}
	var (
		num       = 1
		committed int64
		id        int64
	)
	if len(segs) > 0 {
		last := segs[len(segs)-1]
		n, err := segNumber(last.Name)
		if err != nil {
			return fmt.Errorf("segment name %q: %w", last.Name, err)
		}
		if last.Committed >= segMaxSize {
			num = n + 1 // Rotate; the old segment stays as it is.
		} else {
			num, committed, id = n, last.Committed, last.ID
		}
	}
	if id == 0 {
		if id, err = w.st.newSegment(w.groupID, segFileName(num)); err != nil {
			return err
		}
	}
	path := filepath.Join(w.shard, segDirname, w.group.Dir, segFileName(num))
	seg, err := openSegWriter(path, committed, w.enc)
	if err != nil {
		return err
	}
	w.seg, w.segID, w.segNum, w.segCommitted = seg, id, num, committed
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
	if w.st != nil {
		errs = append(errs, w.st.close())
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
func (w *Writer) SetIdentify(identify *metha.Identify) error {
	w.meta.Identify = identify
	return writeMeta(w.shard, w.meta)
}

// Identify returns the recorded identify response, if there is one.
func (w *Writer) Identify() *metha.Identify { return w.meta.Identify }

// Resume returns the instant a harvest continues from, or the zero time if
// this group holds nothing yet. It is the v2 answer to a readdir over dated
// filenames, and a more exact one: a window that failed, or that could only
// report what existed at the moment it was fetched, hands back its own start,
// so the range is covered again rather than resumed past.
func (w *Writer) Resume() (time.Time, error) {
	unsettled, err := w.st.unsettledFrom(w.groupID)
	if err != nil {
		return time.Time{}, err
	}
	if unsettled != "" {
		return parseWindowTime(unsettled)
	}
	last, err := w.st.lastWindow(w.groupID)
	if err != nil || last == "" {
		return time.Time{}, err
	}
	end, err := parseWindowTime(last)
	if err != nil {
		return time.Time{}, err
	}
	return nextAfter(end), nil
}

// HasWindow reports whether a range has already been harvested, so that a
// re-run can skip it - including a window that yielded no records at all,
// which in v1 had to be remembered by writing an otherwise pointless file.
func (w *Writer) HasWindow(from, until time.Time) (bool, error) {
	return w.st.hasWindow(w.groupID, ts(from), ts(until))
}

// WindowRecords returns how many records the shard has indexed for one range,
// which is what checking an already migrated window against its source needs.
func (w *Writer) WindowRecords(from, until time.Time) (int, error) {
	return w.st.windowRecords(w.groupID, ts(from), ts(until))
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
// as they are; what is parsed out of them only feeds the index.
func (w *Writer) Append(raw []byte) error {
	if w.win == nil {
		return ErrNoWindow
	}
	refs, err := scanRecords(raw)
	if err != nil {
		return fmt.Errorf("scan response: %w", err)
	}
	off := w.seg.append(raw)
	for _, ref := range refs {
		w.win.records = append(w.win.records, recordRow{
			Seg:        w.segID,
			Identifier: ref.Identifier,
			Datestamp:  ref.Datestamp,
			Status:     ref.Status,
			SetSpec:    ref.SetSpec,
			RecOff:     off + ref.Off,
			RecLen:     ref.Len,
			Sum:        ref.Sum,
		})
	}
	w.win.requests++
	w.win.bytes += int64(len(raw))
	if w.seg.pending() >= frameTarget {
		return w.flushFrame()
	}
	return nil
}

// flushFrame writes the buffered responses as one frame and points the records
// that were in it at where it landed.
func (w *Writer) flushFrame() error {
	fr, ok, err := w.seg.flush()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	for i := w.win.unflushed; i < len(w.win.records); i++ {
		w.win.records[i].FrameOff = fr.Off
		w.win.records[i].FrameLen = fr.Len
	}
	w.win.unflushed = len(w.win.records)
	return nil
}

// Commit makes the open window durable: the frames reach the disk first, then
// one transaction records how far the segment is good for, what the window
// cost and every record it carried.
func (w *Writer) Commit() error {
	if w.win == nil {
		return ErrNoWindow
	}
	if err := w.flushFrame(); err != nil {
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
	case len(w.win.records) == 0:
		status = statusEmpty
	}
	row := windowRow{
		GroupID:  w.groupID,
		From:     ts(w.win.from),
		Until:    ts(w.win.until),
		Status:   status,
		Requests: w.win.requests,
		Records:  len(w.win.records),
		Bytes:    w.win.bytes,
		Started:  w.win.started,
		Finished: time.Now().UTC(),
	}
	if err := w.st.commitWindow(row, w.segID, w.seg.size, w.win.records); err != nil {
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
	return w.st.commitWindow(windowRow{
		GroupID:  w.groupID,
		From:     ts(win.from),
		Until:    ts(win.until),
		Status:   statusError,
		Requests: win.requests,
		Bytes:    win.bytes,
		Started:  win.started,
		Finished: time.Now().UTC(),
		Err:      cause.Error(),
	}, 0, 0, nil)
}

// removeV2 forgets one group of a shard: its segments and its rows. The shard
// stays, since other formats and sets of the same endpoint live in it.
func removeV2(baseDir string, id Identity) error {
	shard := shardDir(baseDir, id.BaseURL)
	if !isShard(shard) {
		return nil
	}
	lock, err := metha.TryFlock(filepath.Join(shard, metha.LockName))
	if err != nil {
		return err
	}
	if lock != nil {
		defer lock.Close()
	}
	st, err := openState(filepath.Join(shard, stateName))
	if err != nil {
		return err
	}
	defer st.close()
	groupID, err := st.groupID(id.Format, id.Set)
	if err != nil {
		return err
	}
	if groupID != 0 {
		if err := st.dropGroup(groupID); err != nil {
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
func (w *Writer) SegmentBytes() (int64, error) { return w.st.segmentBytes(w.groupID) }

// Records returns how many records the open window has seen so far.
func (w *Writer) Records() int {
	if w.win == nil {
		return 0
	}
	return len(w.win.records)
}
