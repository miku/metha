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

	"github.com/miku/metha"
)

// marshal renders a response the way the harvester writes it to disk.
func marshal(t *testing.T, resp metha.Response) []byte {
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
	// The segments live under a directory named for the group, so that all of
	// one format and set can be concatenated on their own.
	if want := filepath.Join(s.Dir(), segDirname, "oai_dc"); filepath.Dir(files[0]) != want {
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
	if has, err := w.HasWindow(from, until); err != nil || has {
		t.Fatalf("HasWindow before harvesting: got %v, %v, want false", has, err)
	}
	writeWindow(t, w, "2003-01-01", "2003-12-31")
	has, err := w.HasWindow(from, until)
	if err != nil {
		t.Fatalf("HasWindow: %v", err)
	}
	if !has {
		t.Errorf("HasWindow after an empty harvest: got false, want true")
	}
	seg := filepath.Join(w.Dir(), segDirname, "oai_dc", "000001.zst")
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
	has, err := w.HasWindow(day(t, "2023-02-01"), day(t, "2023-02-28"))
	if err != nil {
		t.Fatalf("HasWindow: %v", err)
	}
	if has {
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
	seg := filepath.Join(shardDir(base, id.BaseURL), segDirname, "oai_dc", "000001.zst")
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

// TestV2RecordOffsets checks the index can address a single record inside a
// frame, which is what an export will seek with instead of scanning.
func TestV2RecordOffsets(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	writeWindow(t, w, "2023-01-01", "2023-01-31", "alpha", "beta", "gamma")
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	st, err := openState(filepath.Join(shardDir(base, id.BaseURL), stateName))
	if err != nil {
		t.Fatalf("openState: %v", err)
	}
	defer st.close()
	rows, err := st.db.Query(`SELECT identifier, frame_off, frame_len, rec_off, rec_len FROM records ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	seg := filepath.Join(shardDir(base, id.BaseURL), segDirname, "oai_dc", "000001.zst")
	var seen int
	for rows.Next() {
		var (
			identifier                      string
			frameOff, frameLen, off, length int64
		)
		if err := rows.Scan(&identifier, &frameOff, &frameLen, &off, &length); err != nil {
			t.Fatalf("scan: %v", err)
		}
		content, err := readFrame(seg, frame{Off: frameOff, Len: frameLen})
		if err != nil {
			t.Fatalf("readFrame: %v", err)
		}
		var rec metha.Record
		if err := xml.Unmarshal(content[off:off+length], &rec); err != nil {
			t.Fatalf("decode record at %d+%d: %v", off, length, err)
		}
		if rec.Header.Identifier != identifier {
			t.Errorf("record at %d+%d: got %q, want %q", off, length, rec.Header.Identifier, identifier)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if seen != 3 {
		t.Errorf("indexed %d records, want 3", seen)
	}
}

// TestV2SecondWriterBlocked: one shard, one writer, so two harvests of the same
// endpoint cannot interleave their windows.
func TestV2SecondWriterBlocked(t *testing.T) {
	base := t.TempDir()
	id := Identity{BaseURL: "http://example.com", Format: "oai_dc"}
	w, err := OpenWriter(base, id)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer w.Close()
	// A different group of the same endpoint still shares the shard lock.
	other := Identity{BaseURL: "http://example.com", Format: "marcxml"}
	if _, err := OpenWriter(base, other); err == nil {
		t.Errorf("second OpenWriter on the same shard: got no error, want one")
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

// TestGroupNameStaysInsideSeg: a group name is one directory component built
// from what the caller asked to harvest, so it must not be a name the
// filesystem has already spoken for. A format of ".." used to come back as
// "..", which put the group's segments in the shard root beside meta.json and
// the index; the empty name joined to nothing and put them in seg/ itself.
func TestGroupNameStaysInsideSeg(t *testing.T) {
	shard := filepath.Join("/cache", "aa", "bb", "hash")
	seg := filepath.Join(shard, segDirname)
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
		dir := filepath.Join(seg, name)
		if got := filepath.Dir(dir); got != seg {
			t.Errorf("groupName(%q, %q) = %q, which lands in %s, want a directory of %s",
				tt.format, tt.set, name, got, seg)
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
			resume, err := w.Resume()
			if err != nil {
				t.Fatalf("Resume: %v", err)
			}
			if want := end.Add(time.Nanosecond); !resume.Equal(want) {
				t.Errorf("Resume: got %v, want %v", resume, want)
			}
			if got, want := resume.In(loc).Format("2006-01-02"), "2023-03-16"; got != want {
				t.Errorf("Resume date in %v: got %v, want %v", name, got, want)
			}
		})
	}
}

// TestCloseLeavesNoSidecarFiles: the index runs in WAL mode, so sqlite keeps a
// write-ahead log and a shared-memory file next to the database while it is
// open, and removes both when the last connection closes. Leaving them behind
// means the last committed window is still in the log rather than in the
// database, and a cache of a quarter million shards carries a sidecar pair per
// shard - so closing the writer has to actually close the database.
func TestCloseLeavesNoSidecarFiles(t *testing.T) {
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
	for _, suffix := range []string{"-wal", "-shm"} {
		path := filepath.Join(shardDir(base, id.BaseURL), stateName+suffix)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists after Close", filepath.Base(path))
		}
	}
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
