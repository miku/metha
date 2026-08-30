package store

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miku/metha/oai"
)

// marshal renders a response the way the harvester writes it to disk.
func marshal(t *testing.T, resp oai.Response) []byte {
	t.Helper()
	b, err := xml.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// day parses a date, for window boundaries. In the local zone, which is where
// the harvester computes them.
func day(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

// writeWindow commits one window holding one response per title.
func writeWindow(t *testing.T, w *Writer, from, until string, titles ...string) {
	t.Helper()
	if err := w.Begin(day(t, from), day(t, until), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, title := range titles {
		if err := w.Append(marshal(t, respWithTitle(title))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// identifiers drains a record stream.
func identifiers(t *testing.T, s Store, opts ReadOptions) []string {
	t.Helper()
	var ids []string
	for rec, err := range s.Records(opts) {
		if err != nil {
			t.Fatalf("Records: %v", err)
		}
		ids = append(ids, rec.Header.Identifier)
	}
	return ids
}

func TestV2RoundTrip(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "first", "second")
	writeWindow(t, w, "2023-02-01", "2023-02-28", "third")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []string{"first", "second", "third"}
	if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, want) {
		t.Errorf("Records: got %v, want %v", got, want)
	}
	last, err := s.Last()
	if err != nil {
		t.Fatalf("Last: %v", err)
	}
	if want := "2023-02-28"; last != want {
		t.Errorf("Last: got %v, want %v", last, want)
	}
	files, err := s.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "000001.zst" {
		t.Errorf("Files: got %v, want one segment named 000001.zst", files)
	}
	// The segments live in a directory named for the group, so that all of
	// one format and set can be concatenated on their own.
	if want := filepath.Join(s.Dir(), "oai_dc"); filepath.Dir(files[0]) != want {
		t.Errorf("segment directory: got %v, want %v", filepath.Dir(files[0]), want)
	}
}

// TestV2EmptyWindowCostsNoBytes is the point of the layout change: a range that
// returned nothing is remembered, and remembering it does not write a file.
func TestV2EmptyWindowCostsNoBytes(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer w.Close()

	from, until := day(t, "2003-01-01"), day(t, "2003-12-31")
	if w.HasWindow(from, until) {
		t.Fatalf("HasWindow before harvesting: got true, want false")
	}
	writeWindow(t, w, "2003-01-01", "2003-12-31")
	if !w.HasWindow(from, until) {
		t.Errorf("HasWindow after an empty harvest: got false, want true")
	}
	seg := filepath.Join(w.Dir(), "oai_dc", "000001.zst")
	info, err := os.Stat(seg)
	if err != nil {
		t.Fatalf("stat segment: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("empty window wrote %d bytes, want 0", info.Size())
	}
}

// TestV2AbortLeavesNoTrace: a window that fails must not leave half its
// responses behind, which in v1 was handled by never renaming the temp files.
func TestV2AbortLeavesNoTrace(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "kept")
	if err := w.Begin(day(t, "2023-02-01"), day(t, "2023-02-28"), true); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.Append(marshal(t, respWithTitle("dropped"))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Abort(errUnderTest); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	// The failed range must not read as harvested, or a retry would skip it.
	if w.HasWindow(day(t, "2023-02-01"), day(t, "2023-02-28")) {
		t.Errorf("HasWindow on an aborted window: got true, want false")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err := Open(base, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, []string{"kept"}) {
		t.Errorf("Records: got %v, want only the committed record", got)
	}
}

// TestV2TornTailTruncated simulates a crash between appending bytes and
// committing them: the segment is longer than the index vouches for, and the
// next open cuts it back.
func TestV2TornTailTruncated(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "committed")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	seg := filepath.Join(shardDir(base, id.BaseURL), "oai_dc", "000001.zst")
	good, err := os.Stat(seg)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Bytes that a dying harvest could have left behind.
	f, err := os.OpenFile(seg, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if _, err := f.Write([]byte("torn tail, never committed")); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()

	w, err = OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter after a crash: %v", err)
	}
	defer w.Close()
	info, err := os.Stat(seg)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != good.Size() {
		t.Errorf("segment is %d bytes, want it truncated back to %d", info.Size(), good.Size())
	}
}

// TestExtentCoversTheWholeWindow: a window's bytes are one run in one segment,
// from the length the index vouched for when it opened to the length it vouches
// for when it commits. That is what replaced addressing records individually,
// and it holds only because nothing else writes into a segment while a window is
// open and a segment rotates only between windows.
func TestExtentCoversTheWholeWindow(t *testing.T) {
	// One frame per response, so a window of three spans several of them.
	old := frameTarget
	frameTarget = 1
	t.Cleanup(func() { frameTarget = old })

	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "alpha", "beta", "gamma")
	writeWindow(t, w, "2023-03-01", "2023-03-31", "delta")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	shard := shardDir(base, id.BaseURL)
	st, err := loadState(statePath(shard, "oai_dc", ""), "oai_dc", "")
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(st.Windows) != 2 {
		t.Fatalf("index holds %d windows, want two", len(st.Windows))
	}
	seg := filepath.Join(shard, "oai_dc", segFileName(1))
	info, err := os.Stat(seg)
	if err != nil {
		t.Fatalf("stat segment: %v", err)
	}
	// One extent each, several frames wide for the first, and between them
	// they account for every byte of the segment: nothing was appended outside
	// a window, and nothing a window appended is left unnamed.
	var covered int64
	for _, win := range st.Windows {
		if len(win.Extents) != 1 {
			t.Fatalf("window %v holds %v, want one extent", win.From, win.Extents)
		}
		if win.Extents[0].Off != covered {
			t.Errorf("extent %+v starts at %d, want it to follow the previous one at %d",
				win.Extents[0], win.Extents[0].Off, covered)
		}
		covered += win.Extents[0].Len
	}
	if covered != info.Size() {
		t.Errorf("extents cover %d bytes of a %d byte segment", covered, info.Size())
	}
	// And a run decodes on its own, without reading what precedes it, which is
	// what makes it addressable at all.
	var got []string
	ok := recordsFromExtent(seg, st.Windows[0].Extents[0], ReadOptions{}, func(rec oai.Record, err error) bool {
		if err != nil {
			t.Errorf("recordsFromExtent: %v", err)
			return false
		}
		got = append(got, rec.Header.Identifier)
		return true
	})
	if !ok {
		t.Fatal("recordsFromExtent stopped early")
	}
	if want := []string{"alpha", "beta", "gamma"}; !slices.Equal(got, want) {
		t.Errorf("decoding the first window's extent: got %v, want %v", got, want)
	}
}

// TestV2SecondWriterBlocked: one group, one writer, so two harvests of the same
// format and set cannot interleave their windows into one index.
func TestV2SecondWriterBlocked(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer w.Close()
	if _, err := OpenWriter(base, id); !errors.Is(err, ErrLocked) {
		t.Errorf("second OpenWriter on the same group: got %v, want ErrLocked", err)
	}
}

// TestV2GroupsDoNotBlockEachOther is the point of moving the lock and the index
// down a level. Two formats of one endpoint are different bytes in different
// files with different indexes, and the only thing they share is meta.json -
// which is written under a lock held for one read-modify-write and released.
// While the lock was the shard's, harvesting an endpoint's oai_dc made its
// marcxml unharvestable for as long as it ran, which for a large repository is
// hours.
func TestV2GroupsDoNotBlockEachOther(t *testing.T) {
	base := t.TempDir()
	dc := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	marc := Identity{BaseURL: "http://example.com", Format: "marcxml"}

	a, err := OpenWriter(base, dc)
	if err != nil {
		t.Fatalf("OpenWriter oai_dc: %v", err)
	}
	defer a.Close()
	b, err := OpenWriter(base, marc)
	if err != nil {
		t.Fatalf("OpenWriter marcxml while oai_dc is open: %v, want both to run", err)
	}
	defer b.Close()

	// Both write, at the same time, into their own segments and their own
	// indexes.
	writeWindow(t, a, "2023-01-01", "2023-01-31", "from-oai-dc")
	writeWindow(t, b, "2023-01-01", "2023-01-31", "from-marcxml")

	// And the shard names both, which is what a listing reads: the group each
	// added under the shard lock survived the other adding theirs.
	meta, err := readMeta(shardDir(base, dc.BaseURL))
	if err != nil {
		t.Fatalf("readMeta: %v", err)
	}
	if len(meta.Groups) != 2 {
		t.Errorf("meta names %v, want both groups", meta.Groups)
	}
}

func TestV2Groups(t *testing.T) {
	base := t.TempDir()
	ids := []Identity{
		{BaseURL: "http://example.com", Format: "oai_dc"},
		{BaseURL: "http://example.com", Format: "oai_dc", Set: "kau:science"},
	}
	for i, id := range ids {
		w, err := OpenWriter(base, id)
		if err != nil {
			t.Fatalf("OpenWriter: %v", err)
		}
		writeWindow(t, w, "2023-01-01", "2023-01-31", "group"+string(rune('a'+i)))
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	// Both groups live in one shard, each with its own segments.
	for i, id := range ids {
		s, err := Open(base, id)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		want := []string{"group" + string(rune('a'+i))}
		if got := identifiers(t, s, ReadOptions{}); !slices.Equal(got, want) {
			t.Errorf("Records for %v: got %v, want %v", id, got, want)
		}
	}
	// A listing reports one entry per group, from meta.json alone.
	var found []Identity
	for entry, err := range List(base) {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found = append(found, entry.Identity)
	}
	for _, id := range ids {
		if !slices.Contains(found, id) {
			t.Errorf("List: missing %v, got %v", id, found)
		}
	}
}

func TestGroupName(t *testing.T) {
	for _, tt := range []struct {
		format, set string
		want        string
	}{
		{"oai_dc", "", "oai_dc"},
		{"marcxml", "abc", "marcxml+abc"},
		// A colon is ordinary in a setSpec but awkward in a path, so the name
		// is escaped and a digest keeps it unique.
		{"oai_dc", "kau:science", "oai_dc+kau-science-"},
	} {
		got := groupName(tt.format, tt.set)
		if !strings.HasPrefix(got, tt.want) {
			t.Errorf("groupName(%q, %q): got %v, want prefix %v", tt.format, tt.set, got, tt.want)
		}
	}
	// Escaping must not let two different sets land in one directory.
	a := groupName("oai_dc", "x:y")
	b := groupName("oai_dc", "x-y")
	if a == b {
		t.Errorf("groupName collision: %q and %q both map to %v", "x:y", "x-y", a)
	}
	// Long names stay usable.
	if got := groupName("oai_dc", strings.Repeat("long", 100)); len(got) > 80 {
		t.Errorf("groupName of a very long set is %d chars: %v", len(got), got)
	}
}

// TestGroupNameStaysInsideTheShard: a group name is one directory component
// built from what the caller asked to harvest, so it must not be a name the
// filesystem has already spoken for. A format of ".." used to come back as
// "..", which landed a level above where it belongs; the empty name joined to
// nothing and put the group's files in the directory above it.
//
// It matters more now than it did, because a group directory holds the group's
// index and lock as well as its segments: a name that escapes would put them
// beside another group's, or beside the shard's own meta.json.
func TestGroupNameStaysInsideTheShard(t *testing.T) {
	shard := filepath.Join("/cache", "aa", "bb", "hash")
	for _, tt := range []struct{ format, set string }{
		{".", ""},
		{"..", ""},
		{"", ""},
		{"", ".."},
		{"..", ".."},
		{"../../etc", ""},
		{".", "."},
	} {
		name := groupName(tt.format, tt.set)
		dir := groupDir(shard, tt.format, tt.set)
		if got := filepath.Dir(dir); got != shard {
			t.Errorf("groupName(%q, %q) = %q, which lands in %s, want a directory of %s",
				tt.format, tt.set, name, got, shard)
		}
		if !safeComponent(name) {
			t.Errorf("groupName(%q, %q) = %q, which is not a usable directory name",
				tt.format, tt.set, name)
		}
	}
	// Different pathological inputs must still not collide.
	seen := map[string]string{}
	for _, format := range []string{".", "..", "", "...", "-"} {
		name := groupName(format, "")
		if was, dup := seen[name]; dup {
			t.Errorf("groupName(%q) and groupName(%q) both map to %q", format, was, name)
		}
		seen[name] = format
	}
	// And the ordinary names are untouched.
	if got := groupName("oai_dc", ""); got != "oai_dc" {
		t.Errorf("groupName(oai_dc): got %q, want it left alone", got)
	}
}

// errUnderTest stands in for whatever made a harvest give up on a window.
var errUnderTest = errors.New("harvest failed under test")

// TestResumeAcrossZones: a window ends at the close of a local day, and that
// instant usually falls on a different UTC date. Resuming has to land on the
// start of the next local day, or a harvest would skip or repeat one depending
// on where it runs.
func TestResumeAcrossZones(t *testing.T) {
	for _, name := range []string{"UTC", "Europe/Vienna", "America/Chicago", "Pacific/Auckland"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Skipf("no zoneinfo for %v: %v", name, err)
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
			w, err := OpenWriter(base, id)
			if err != nil {
				t.Fatalf("OpenWriter: %v", err)
			}
			defer w.Close()
			// The end of 2023-03-15, as the harvester computes it.
			end := time.Date(2023, 3, 15, 23, 59, 59, int(time.Second-1), loc)
			if err := w.Begin(time.Date(2023, 3, 1, 0, 0, 0, 0, loc), end, true); err != nil {
				t.Fatalf("Begin: %v", err)
			}
			if err := w.Append(marshal(t, respWithTitle("x"))); err != nil {
				t.Fatalf("Append: %v", err)
			}
			if err := w.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			resume := w.Resume()
			if want := end.Add(time.Nanosecond); !resume.Equal(want) {
				t.Errorf("Resume: got %v, want %v", resume, want)
			}
			if got, want := resume.In(loc).Format("2006-01-02"), "2023-03-16"; got != want {
				t.Errorf("Resume date in %v: got %v, want %v", name, got, want)
			}
		})
	}
}

// TestShardHoldsOnlyWhatItNeeds: a harvested shard is what the endpoint is, the
// lock over that, and one directory per group; a group is its index, its lock,
// and the bytes. Anything else is a file a quarter million shards each carry a
// copy of, which is how the write-ahead log and the shared-memory sidecar of the
// sqlite index used to be paid for.
func TestShardHoldsOnlyWhatItNeeds(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "committed")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	shard := shardDir(base, id.BaseURL)
	// The shard itself: what the endpoint is, the lock that guards that, and the
	// group. Nothing about any one format lives at this level any more.
	if want := []string{LockName, metaName, "oai_dc"}; !slices.Equal(dirNames(t, shard), want) {
		t.Errorf("shard holds %v, want %v", dirNames(t, shard), want)
	}
	// The group: its own lock, its own index, its own bytes.
	group := groupDir(shard, id.Format, id.Set)
	if want := []string{segFileName(1), LockName, stateName}; !slices.Equal(dirNames(t, group), want) {
		t.Errorf("group holds %v, want %v", dirNames(t, group), want)
	}
}

// dirNames lists what a directory holds, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

// TestLayoutsShareTheCache: shards sit directly in the cache, with no directory
// naming the layout above them, so during a migration a pre-1.0 endpoint
// directory and a shard fan-out directory are siblings. A listing must not
// claim the other's entries, and must not descend into them. What keeps them
// apart is the shape of the name: a fan-out is two hex digits, and base64 of
// "set#format#baseURL" is never that short.
func TestLayoutsShareTheCache(t *testing.T) {
	base := t.TempDir()
	legacyOnly := Identity{BaseURL: "http://example.com/legacy", Format: "oai_dc"}
	sharded := Identity{BaseURL: "http://example.com/sharded", Format: "oai_dc"}

	dir := legacyDir(base, legacyOnly)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	createFile(t, dir, "2023-01-31-00000001.xml.zst", createZstdWriter, twoRecords())

	w, err := OpenWriter(base, sharded)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "sharded")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The shard is a sibling of the old directory, not inside a v2/ of its own.
	if got, want := filepath.Dir(filepath.Dir(filepath.Dir(w.Dir()))), base; got != want {
		t.Errorf("shard sits at %s, want three levels under %s", w.Dir(), base)
	}

	var seen []Identity
	for entry, err := range List(base) {
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		seen = append(seen, entry.Identity)
	}
	if want := []Identity{sharded}; !slices.Equal(seen, want) {
		t.Errorf("List: got %v, want %v; the pre-1.0 directory is counted separately", seen, want)
	}
	if got := LegacyRemainder(base); got != 1 {
		t.Errorf("LegacyRemainder: got %d, want 1", got)
	}
}

// TestIsShardPrefix: the whole of what tells a fan-out directory from an
// endpoint directory.
func TestIsShardPrefix(t *testing.T) {
	for name, want := range map[string]bool{
		"af": true, "00": true, "ff": true, "9c": true,
		"AF": false, "g0": false, "a": false, "abc": false, "": false,
		"aG9tZQ": false, // base64, the shape a v1 directory has
	} {
		if got := isShardPrefix(name); got != want {
			t.Errorf("isShardPrefix(%q) = %v, want %v", name, got, want)
		}
	}
}
