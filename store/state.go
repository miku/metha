package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"
)

// state is a shard's index: which ranges have been harvested, and which bytes
// of which segments hold what came back. It is derived data - every field can
// be rebuilt by rereading the segments - which is what keeps "metha is a cache"
// true and recovery down to a rebuild.
//
// One JSON file, written whole and renamed into place. That is the trick v1 got
// for free by renaming a data file, made explicit rather than implied by a
// filename: a reader sees the previous index or this one and never half of one,
// and bytes appended past a segment's committed length are the tail of a harvest
// that died mid-window, dropped on the next open.
//
// Writing it whole is affordable because it stays small. Settled windows that
// touch are merged, so a decade of daily harvests is one window, and the bytes
// each commit appended join onto the run already there whenever they follow it
// directly - which they do unless a superseded copy sits in between. The record
// level is deliberately not here: a window is the unit of atomicity everywhere
// else, so it is the unit of addressing too, and a read decodes whole extents
// rather than seeking to individual records.
type state struct {
	path string

	Version int           `json:"version"`
	Groups  []*groupState `json:"groups"`
}

// stateVersion is the shape of the index this code writes. It is in the file so
// that a shard and the binary opening it can disagree out loud instead of
// quietly.
const stateVersion = 1

// groupState is what a shard holds for one format and set. The directory the
// segments live in is not repeated here: it is derived from the format and set
// by groupName, and meta.json is the shard's account of which groups exist.
type groupState struct {
	Format   string       `json:"format"`
	Set      string       `json:"set,omitempty"`
	Segments []*segRow    `json:"segments,omitempty"`
	Windows  []*windowRow `json:"windows,omitempty"`
}

// segRow is a segment file and the prefix of it the index vouches for. The name
// is not stored because it is segFileName(Num), and a number that has to agree
// with a name is one more thing that can disagree.
type segRow struct {
	Num       int   `json:"num"`
	Committed int64 `json:"committed"`
}

// extent is a run of bytes in one segment: the whole of what one commit
// appended. It is a whole number of frames by construction - appends happen only
// inside a window and segments rotate only between them - so an extent is a
// valid zstd stream on its own and can be decoded without reading what precedes
// it.
type extent struct {
	Seg int   `json:"seg"`
	Off int64 `json:"off"`
	Len int64 `json:"len"`
}

// windowRow is one harvested range: what it covers, what it cost, what it
// yielded, and where the bytes it brought back are.
//
// Boundaries are instants, stored in UTC and compared as time rather than as
// text, so nothing depends on how they are spelled. A window with no boundaries
// at all - the one a -no-intervals harvest writes, which covers whatever the
// endpoint chose to send - is the zero time at both ends.
type windowRow struct {
	From     time.Time `json:"from"`
	Until    time.Time `json:"until"`
	Status   string    `json:"status"`
	Requests int       `json:"requests"`
	Records  int       `json:"records"`
	Deleted  int       `json:"deleted"`
	Bytes    int64     `json:"bytes"`
	Elapsed  int64     `json:"elapsed_ns"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Err      string    `json:"err,omitempty"`

	// Lo and Hi bracket the datestamps of the records this window holds,
	// widened to whole seconds so the two granularities OAI-PMH allows compare
	// as text. They are what a filtered read prunes on, and they are exact
	// where the window's own boundaries are only a request: an endpoint that
	// ignores from and until returns records outside the range it was asked
	// for, and pruning on what was asked would then drop them.
	//
	// An empty Lo means the window cannot be pruned - either it holds no
	// records, or one of them carried a datestamp in a shape no bound can be
	// ordered against. A filter the index cannot reason about is answered by
	// reading more, never by reading less.
	Lo string `json:"lo,omitempty"`
	Hi string `json:"hi,omitempty"`

	Extents []extent `json:"extents,omitempty"`
}

// Window statuses. An empty window is a first class outcome: it costs a row and
// no bytes, where v1 had to write a file to remember it at all.
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

// settled reports whether a status describes a range that is final, and so one
// that may be folded into its neighbour. A window that failed or that only
// holds what existed at the moment of asking has to keep its own boundaries,
// because those boundaries are where the next run resumes.
func settled(status string) bool { return status == statusOK || status == statusEmpty }

// loadState reads a shard's index. A shard that has never committed a window
// has no file yet, and that is the empty index rather than an error: the first
// commit writes one, so an endpoint whose harvest died before it got anywhere
// costs nothing at all.
func loadState(path string) (*state, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &state{path: path, Version: stateVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var s state
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version != stateVersion {
		return nil, fmt.Errorf("%s: index is version %d, this metha writes %d", path, s.Version, stateVersion)
	}
	s.path = path
	return &s, nil
}

// save writes the index out whole and renames it into place. That rename is the
// entirety of what makes a commit atomic: the segment bytes are already on disk
// and already synced, and this is the statement that they count.
//
// Windows are sorted by their start first, so that the file reads in the order
// the ranges do. Nothing depends on the order - every query here is a scan over
// a handful of rows - which is why sorting can be a property of the file rather
// than an invariant the mutations have to maintain.
func (s *state) save() error {
	s.Version = stateVersion
	for _, g := range s.Groups {
		slices.SortFunc(g.Windows, func(a, b *windowRow) int { return a.From.Compare(b.From) })
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path, append(b, '\n'))
}

// group returns the state of one format and set, or nil if the shard holds
// none.
func (s *state) group(format, set string) *groupState {
	for _, g := range s.Groups {
		if g.Format == format && g.Set == set {
			return g
		}
	}
	return nil
}

// ensureGroup returns the state of one format and set, creating it if this is
// the first time it is harvested into the shard.
func (s *state) ensureGroup(format, set string) *groupState {
	if g := s.group(format, set); g != nil {
		return g
	}
	g := &groupState{Format: format, Set: set}
	s.Groups = append(s.Groups, g)
	return g
}

// dropGroup forgets a group entirely: its windows and its segments. The segment
// files are the caller's to delete.
func (s *state) dropGroup(g *groupState) error {
	s.Groups = slices.DeleteFunc(s.Groups, func(have *groupState) bool { return have == g })
	return s.save()
}

// segment returns a group's record of one segment file, or nil.
func (g *groupState) segment(num int) *segRow {
	for _, seg := range g.Segments {
		if seg.Num == num {
			return seg
		}
	}
	return nil
}

// segmentBytes returns how many bytes of a group's segments the index vouches
// for. That is the committed length rather than the file size, so the torn tail
// of a crashed harvest, which the next open truncates, is not counted.
func (g *groupState) segmentBytes() int64 {
	var n int64
	for _, seg := range g.Segments {
		n += seg.Committed
	}
	return n
}

// lastWindow returns the largest window end of a group, or the zero time if
// nothing was harvested into it yet. This is the resume point when every window
// is settled.
//
// The boundless window of a -no-intervals harvest ends at the zero time, so it
// loses the maximum to any real boundary. A group that holds nothing else
// answers the zero time, and a harvest that switches to intervals starts from
// the endpoint's earliest date - the only honest answer, since an unbounded
// fetch says nothing about what it covered.
func (g *groupState) lastWindow() time.Time {
	var last time.Time
	for _, w := range g.Windows {
		if w.Status != statusError && w.Until.After(last) {
			last = w.Until
		}
	}
	return last
}

// unsettledFrom returns the start of the earliest window that is not final -
// one that failed, or one that covered a range the endpoint could still add to
// - or the zero time when every window is settled. It is the resume point
// whenever there is one, ahead of lastWindow: resuming past a window that only
// holds what existed at the time of asking is how updates go missing.
//
// This is also what retries a window that failed in the middle of a range,
// which a high water mark alone can only do for the newest one.
//
// A window with no boundaries is skipped. It has no start to resume from, and
// it makes no claim about which ranges are covered, so it must not answer a
// question about them either.
func (g *groupState) unsettledFrom() time.Time {
	var first time.Time
	for _, w := range g.Windows {
		if settled(w.Status) || w.From.IsZero() {
			continue
		}
		if first.IsZero() || w.From.Before(first) {
			first = w.From
		}
	}
	return first
}

// hasWindow reports whether a range was already harvested successfully, which
// is what makes a re-run skip work instead of refetching it.
//
// A range is covered when some settled window contains it, not when one matches
// it exactly: settled windows are merged as they are committed, so the row that
// answers for a range is usually wider than the range, and after a year of
// daily harvests it is a great deal wider.
func (g *groupState) hasWindow(from, until time.Time) bool {
	for _, w := range g.Windows {
		if settled(w.Status) && !w.From.After(from) && !w.Until.Before(until) {
			return true
		}
	}
	return false
}

// countRecords returns how many records the group holds, summed over the
// windows that claim them. A migration compares this against its source.
func (g *groupState) countRecords() int {
	var n int
	for _, w := range g.Windows {
		n += w.Records
	}
	return n
}

// windowRecords returns how many records the windows lying inside a range hold.
//
// Windows inside the range, rather than one matching it: merging means a
// migrated cache answers out of a single row spanning everything it holds, and
// asking about one day of it would find nothing. A window reaching past the
// range is left out, so a shard that has been harvested further reports short
// rather than counting records the range never claimed.
func (g *groupState) windowRecords(from, until time.Time) int {
	var n int
	for _, w := range g.Windows {
		if !w.From.Before(from) && !w.Until.After(until) {
			n += w.Records
		}
	}
	return n
}

// commitWindow makes a harvested window real: the segment's committed length,
// the window and the extent naming its bytes reach the disk in one rename or
// not at all. Bytes appended past that length are the torn tail of a crash and
// are dropped on the next open.
func (s *state) commitWindow(g *groupState, win windowRow, segNum int, segSize int64) error {
	if seg := g.segment(segNum); seg != nil {
		seg.Committed = segSize
	}
	g.supersede(win)
	if !g.extendSettled(win) {
		g.Windows = append(g.Windows, &win)
	}
	return s.save()
}

// supersede drops what a window about to be committed replaces: the row
// covering exactly the same range, and any unsettled row that starts inside it.
//
// The exact match is a refetch of the same range. The unsettled ones are ranges
// that were only ever partly answered and have just been fetched again, under
// whatever boundaries this run chose - usually the same, but not when the
// interval size changed between runs, and then a stale row would hold the
// resume point back forever. Settled rows are left alone: they are answers that
// still stand.
//
// Their bytes stay in the segment, since the blob layer is append-only and
// dedupe is a reader's decision. What goes is the extent naming them, and a run
// of bytes no extent names is one no read can reach.
func (g *groupState) supersede(win windowRow) {
	g.Windows = slices.DeleteFunc(g.Windows, func(w *windowRow) bool {
		if w.From.Equal(win.From) && w.Until.Equal(win.Until) {
			return true
		}
		return !settled(w.Status) && !w.From.Before(win.From) && !w.From.After(win.Until)
	})
}

// extendSettled folds a settled window into the settled window that ends where
// it begins, and reports whether it found one.
//
// A settled window that begins where a settled window ends is one stretch of
// time fetched in two goes, and the index is never asked how many goes it took -
// only which ranges are covered. Folding them keeps this a map of coverage
// rather than a log of invocations: a group harvested daily for a decade holds
// one row instead of three and a half thousand.
//
// What it costs is the per-window detail. Requests, bytes and duration become
// sums over the merged range, and the range itself stops recording where one run
// stopped and the next began. The row keeps the started of the earlier half,
// since that is when the merged range was first reached for, and takes the
// finished of the later one.
func (g *groupState) extendSettled(win windowRow) bool {
	// A window with no boundaries covers whatever the endpoint chose to send
	// and abuts nothing; see unsettledFrom.
	if !settled(win.Status) || win.From.IsZero() {
		return false
	}
	for _, w := range g.Windows {
		if !settled(w.Status) || !w.Until.Equal(win.From.Add(-time.Nanosecond)) {
			continue
		}
		w.Lo, w.Hi = mergeStamps(w, &win)
		w.Until = win.Until
		w.Requests += win.Requests
		w.Records += win.Records
		w.Deleted += win.Deleted
		w.Bytes += win.Bytes
		w.Elapsed += win.Elapsed
		w.Finished = win.Finished
		w.Status = statusOK
		if w.Records == 0 {
			w.Status = statusEmpty
		}
		w.addExtents(win.Extents)
		return true
	}
	return false
}

// mergeStamps brackets the datestamps of two windows that are becoming one. A
// window holding no records says nothing about datestamps and so contributes
// nothing; one that could not be bracketed at all makes the result unbounded,
// because a range that cannot be reasoned about does not become smaller by
// being merged into one that can.
func mergeStamps(a, b *windowRow) (lo, hi string) {
	switch {
	case a.Records == 0:
		return b.Lo, b.Hi
	case b.Records == 0:
		return a.Lo, a.Hi
	}
	return coverStamps(a.Lo, a.Hi, b.Lo, b.Hi)
}

// coverStamps merges two datestamp brackets. An empty pair is one that could not
// be bracketed at all, and it swallows the other: a range that cannot be
// reasoned about does not become narrower by being merged into one that can.
func coverStamps(aLo, aHi, bLo, bHi string) (lo, hi string) {
	if aLo == "" || bLo == "" {
		return "", ""
	}
	return min(aLo, bLo), max(aHi, bHi)
}

// addExtents appends bytes to a window, joining them onto the run already there
// when they follow it directly. Harvesting a range over several runs appends to
// the same segment at the point the last commit stopped, so the common case is
// one extent per window however many goes it took; a second extent appears only
// where the bytes of a superseded window sit in between.
func (w *windowRow) addExtents(exts []extent) {
	for _, e := range exts {
		if n := len(w.Extents); n > 0 {
			last := &w.Extents[n-1]
			if last.Seg == e.Seg && last.Off+last.Len == e.Off {
				last.Len += e.Len
				continue
			}
		}
		w.Extents = append(w.Extents, e)
	}
}

// canMatch reports whether a window could hold a record the filter accepts. It
// prunes; it never decides. Every record that comes out of an extent is still
// checked against the filter, so an index that has fallen behind the segments
// can cost time but cannot produce a wrong answer.
func (w *windowRow) canMatch(opts ReadOptions) bool {
	switch opts.Deleted {
	case DeletedOnly:
		if w.Deleted == 0 {
			return false
		}
	case DeletedSkip:
		if w.Records > 0 && w.Deleted == w.Records {
			return false
		}
	}
	if w.Lo == "" {
		return true
	}
	if opts.From != "" && w.Hi < widen(opts.From, dayStart) {
		return false
	}
	if opts.Until != "" && w.Lo > widen(opts.Until, dayEnd) {
		return false
	}
	return true
}

// liveExtents returns the byte runs a read has to visit, in the order they were
// written. Superseded copies are named by no window and so appear nowhere here,
// which is what makes a read a view of the cache rather than a dump of it.
func (g *groupState) liveExtents(opts ReadOptions) []extent {
	var exts []extent
	for _, w := range g.Windows {
		if w.canMatch(opts) {
			exts = append(exts, w.Extents...)
		}
	}
	slices.SortFunc(exts, func(a, b extent) int {
		if a.Seg != b.Seg {
			return cmp.Compare(a.Seg, b.Seg)
		}
		return cmp.Compare(a.Off, b.Off)
	})
	return exts
}

// windowDate renders a boundary as a date, which is what a listing shows for
// how far an endpoint got. Into local time: boundaries are the edges of local
// days, and in most of the world such an instant belongs to a different UTC
// date, so a caller that goes on to think in days would be off by one.
func windowDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(time.Local).Format("2006-01-02")
}

// nextAfter returns where a harvest picks up after a settled window ended at t.
// Both OAI bounds are inclusive, so the next window starts just past this one,
// or the boundary would be fetched twice. Every boundary a window is recorded
// with is exact - a date-only bound is stored as the end of the day it stands
// for - so a nanosecond is all it takes.
func nextAfter(t time.Time) time.Time {
	return t.Add(time.Nanosecond)
}
