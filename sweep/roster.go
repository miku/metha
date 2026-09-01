package sweep

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

// The three files a sweep keeps, beside the shards in the cache directory so
// that the state moves with the data it describes.
const (
	// RosterName is the roster proper: zstd-compressed JSONL, a header line
	// followed by one profile per line, sorted by URL. Rewritten whole.
	RosterName = "sweep.json.zst"
	// JournalName is where outcomes land while a sweep runs. Same records, same
	// encoding, no header, appended to.
	JournalName = "sweep.journal"
	// LockName keeps two sweeps from overlapping. A timer firing over a long
	// sweep finds it held and exits, rather than starting a second one.
	LockName = "SWEEP.LOCK"
)

// rosterVersion is the shape of the file this code writes. It is in the file so
// that a roster and the binary opening it can disagree out loud instead of
// quietly, which is the same reason store's index carries one.
const rosterVersion = 1

var (
	// ErrVersion marks a roster written by a different version of this code.
	ErrVersion = errors.New("sweep: unsupported roster version")
	// ErrIdentity marks a roster describing a different sweep. See Header.
	ErrIdentity = errors.New("sweep: roster describes another format or set")
	// ErrMalformed marks a file that is not a roster at all.
	ErrMalformed = errors.New("sweep: not a roster")
)

// Header is the roster's first line: what sweep these rows are about.
//
// The format and set are here, once, rather than on every profile, because a
// roster describes one sweep and a sweep harvests one format. That is what
// keeps a URL sufficient as a key. It is also the guard that makes a later
// multi-format sweep safe: it reads this line, sees the rows are not about it,
// and says so - rather than silently reinterpreting a quarter of a million
// records as though they were.
type Header struct {
	Version   int       `json:"version"`
	Format    string    `json:"format"`
	Set       string    `json:"set,omitempty"`
	Endpoints int       `json:"endpoints"`
	Compacted time.Time `json:"compacted,omitzero"`
}

// Roster is every endpoint the sweep has ever heard of, and what became of it.
//
// It is deliberately not the truth about what was harvested. The cache is that,
// and where the two disagree the roster is the one that gets corrected: a user
// is free to harvest any endpoint by hand, and the roster catches up at the
// next sweep. What the roster owns is the other question - what was
// *attempted*, including everything that failed - and that has to live outside
// the store, because a harvest that harvested nothing deliberately leaves no
// shard behind. Failure memory is the scheduler's business, not the cache's.
//
// Durability is a journal plus periodic compaction, which is the same trick the
// segments use. Every outcome is appended as one line; the roster proper is
// rewritten whole, rarely. 244k records at a couple of hundred bytes is around
// 50MB of highly repetitive JSON, which zstd takes to a few megabytes - small
// enough that rewriting it once a sweep is not worth thinking about, and far
// too large to rewrite after every endpoint.
//
// Safe for concurrent use: the runner has a worker pool and this is the one
// thing they all write to.
type Roster struct {
	mu       sync.Mutex
	dir      string
	header   Header
	profiles map[string]*Profile

	journal *os.File
	buf     *bufio.Writer

	// Now is the clock, replaced in tests.
	Now func() time.Time
}

// Open loads the roster for one format and set, replaying anything a previous
// sweep left in the journal.
//
// A directory with no roster yields an empty one rather than an error: that is
// the state of a machine that has not swept yet, and Seed is what fills it.
func Open(dir, format, set string) (*Roster, error) {
	r := &Roster{
		dir:      dir,
		profiles: make(map[string]*Profile),
		header:   Header{Version: rosterVersion, Format: format, Set: set},
		Now:      func() time.Time { return time.Now().UTC() },
	}
	if err := r.load(); err != nil {
		return nil, err
	}
	// A journal found at startup is the tail of a sweep that was killed. Folding
	// it in is the same operation as the ordinary load, which is the point:
	// there is no separate recovery path here that could rot for want of ever
	// being taken.
	replayed, err := r.replay()
	if err != nil {
		return nil, err
	}
	if replayed > 0 {
		if err := r.compact(); err != nil {
			return nil, err
		}
	}
	if err := r.openJournal(); err != nil {
		return nil, err
	}
	return r, nil
}

// Load reads the roster without taking it over: the header as it stands and
// every profile, sorted by URL.
//
// Open is for a sweep. It creates the journal, folds in whatever a killed run
// left behind and compacts, all of which are writes - and a view that rewrites
// the file it is reading is a view that can lose the work of a sweep running
// beside it. This one only reads, so "metha endpoints" needs no lock and costs
// a running sweep nothing.
//
// The journal is still replayed, because a sweep that is running right now has
// its outcomes there and not yet in the roster proper. Reading it is how a
// listing taken mid-sweep is current rather than a day old; what is left out is
// only the writing back.
func Load(dir, format, set string) (Header, []Profile, error) {
	r := &Roster{
		dir:      dir,
		profiles: make(map[string]*Profile),
		header:   Header{Version: rosterVersion, Format: format, Set: set},
		Now:      func() time.Time { return time.Now().UTC() },
	}
	if err := r.load(); err != nil {
		return Header{}, nil, err
	}
	if _, err := r.replay(); err != nil {
		return Header{}, nil, err
	}
	return r.Header(), r.Profiles(), nil
}

// load reads the roster proper.
func (r *Roster) load() error {
	f, err := os.Open(filepath.Join(r.dir, RosterName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer dec.Close()

	sc := bufio.NewScanner(dec)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return err
		}
		// An empty roster file is an empty roster. It is what a compaction
		// interrupted at the worst moment could leave, and there is nothing in
		// it to disagree with.
		return nil
	}
	var h Header
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMalformed, RosterName, err)
	}
	// A profile line decodes into a Header with a zero version, so this catches
	// a file whose header is missing as well as one that was never a roster.
	if h.Version == 0 {
		return fmt.Errorf("%w: %s: no header", ErrMalformed, RosterName)
	}
	if h.Version != rosterVersion {
		return fmt.Errorf("%w: %s has version %d, this metha writes %d",
			ErrVersion, RosterName, h.Version, rosterVersion)
	}
	if h.Format != r.header.Format || h.Set != r.header.Set {
		return fmt.Errorf("%w: %s is about %q/%q, this sweep is %q/%q",
			ErrIdentity, RosterName, h.Format, h.Set, r.header.Format, r.header.Set)
	}
	for sc.Scan() {
		if err := r.decodeInto(sc.Bytes()); err != nil {
			return fmt.Errorf("%s: %w", RosterName, err)
		}
	}
	return sc.Err()
}

// replay folds a leftover journal into the loaded profiles and reports how many
// lines it took.
//
// Last line wins, which is what makes replaying twice a no-op: if a crash lands
// between the rename that publishes a compacted roster and the removal of the
// journal it replaced, the next Open replays records the roster already holds
// and nothing moves. Idempotent by construction rather than by a flag that has
// to be written at the right moment.
func (r *Roster) replay() (int, error) {
	f, err := os.Open(filepath.Join(r.dir, JournalName))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	var n int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := r.decodeInto(line); err != nil {
			// A torn last line is what a killed sweep leaves, and it is the
			// expected end of a journal rather than a corruption to refuse. Only
			// the outcome it described is lost, and that endpoint is simply
			// attempted again.
			break
		}
		n++
	}
	return n, sc.Err()
}

// maxLine bounds one JSON line. A profile is a couple of hundred bytes; the
// only field that can grow is LastError, which is truncated on the way in.
const maxLine = 1 << 20

// decodeInto adds one profile line to the map.
func (r *Roster) decodeInto(line []byte) error {
	var p Profile
	if err := json.Unmarshal(line, &p); err != nil {
		return err
	}
	if p.URL == "" {
		return fmt.Errorf("%w: profile without a url", ErrMalformed)
	}
	r.profiles[p.URL] = &p
	return nil
}

// openJournal opens the append-only log for this sweep.
func (r *Roster) openJournal() error {
	f, err := os.OpenFile(filepath.Join(r.dir, JournalName),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	r.journal, r.buf = f, bufio.NewWriterSize(f, 64*1024)
	return nil
}

// Seed adds URLs the roster has not seen, as new endpoints due immediately, and
// reports how many were added. URLs it already holds are left exactly as they
// are - seeding is not a way to reset a profile, or importing the shipped list
// again would undo every failure the sweep has learned.
func (r *Roster) Seed(urls []string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.Now().UTC()
	var added int
	for _, u := range urls {
		if u == "" || r.profiles[u] != nil {
			continue
		}
		p := Profile{URL: u, State: StateNew, FirstSeen: now}
		r.profiles[u] = &p
		if err := r.append(p); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// Get returns a copy of one profile.
func (r *Roster) Get(url string) (Profile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.profiles[url]
	if !ok {
		return Profile{}, false
	}
	return *p, true
}

// Put records a profile: into the map, and onto the journal.
//
// Both, always, and in that order - the roster in memory is what the next
// selection reads and the journal is what survives a kill. A sweep interrupted
// at any moment therefore loses at most the endpoints in flight plus whatever
// is still buffered; see Flush.
func (r *Roster) Put(p Profile) error {
	if p.URL == "" {
		return fmt.Errorf("%w: profile without a url", ErrMalformed)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c := p
	r.profiles[p.URL] = &c
	return r.append(p)
}

// append writes one line. The caller holds the lock.
func (r *Roster) append(p Profile) error {
	if r.buf == nil {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err := r.buf.Write(b); err != nil {
		return err
	}
	return r.buf.WriteByte('\n')
}

// Flush pushes the journal's buffer to the kernel.
//
// Not fsync: 244k of those would cost more than the sweep does. What this
// protects against is the failure that actually happens - a SIGKILL from a
// timer or an operator - where the page cache survives and the file is intact.
// A machine that loses power mid-sweep loses the un-flushed tail, and those
// endpoints are attempted again on the next run. That is the trade, and it is
// deliberate.
func (r *Roster) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buf == nil {
		return nil
	}
	return r.buf.Flush()
}

// Len is how many endpoints the roster holds.
func (r *Roster) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.profiles)
}

// Profiles returns every profile, sorted by URL, as copies. This is what gets
// handed to a Selector, and copies are what let the selector be a pure function
// over a slice rather than something holding a lock while it thinks.
func (r *Roster) Profiles() []Profile {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Profile, 0, len(r.profiles))
	for _, p := range r.profiles {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b Profile) int {
		return cmp.Compare(a.URL, b.URL)
	})
	return out
}

// Header returns the roster's header as it would be written now.
func (r *Roster) Header() Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.header
	h.Endpoints = len(r.profiles)
	return h
}

// Compact rewrites the roster from what is in memory and drops the journal.
func (r *Roster) Compact() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.compact()
}

// compact does the work. The caller holds the lock, except during Open, where
// there is nothing yet to contend with it.
//
// The order is the whole of the crash story: write a temporary file, sync it,
// rename it over the roster, and only then remove the journal it replaced. A
// crash before the rename leaves the old roster and the whole journal, which
// replays to the same place. A crash after the rename but before the removal
// leaves a journal whose records the new roster already holds, which replays to
// the same place again. There is no window in which an outcome is in neither
// file.
func (r *Roster) compact() error {
	if r.buf != nil {
		if err := r.buf.Flush(); err != nil {
			return err
		}
	}
	h := r.header
	h.Version = rosterVersion
	h.Endpoints = len(r.profiles)
	h.Compacted = r.Now().UTC()

	profiles := make([]*Profile, 0, len(r.profiles))
	for _, p := range r.profiles {
		profiles = append(profiles, p)
	}
	slices.SortFunc(profiles, func(a, b *Profile) int {
		return cmp.Compare(a.URL, b.URL)
	})

	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	if err := enc.Encode(h); err != nil {
		return err
	}
	for _, p := range profiles {
		if err := enc.Encode(p); err != nil {
			return err
		}
	}
	if err := writeZstdAtomic(filepath.Join(r.dir, RosterName), body.Bytes()); err != nil {
		return err
	}
	// The journal is dropped last, and the handle onto it goes with it: the
	// file this process is appending to has just been unlinked, so anything
	// written through the old handle would go to a file no one can open.
	if r.journal != nil {
		_ = r.journal.Close()
		r.journal, r.buf = nil, nil
	}
	if err := os.Remove(filepath.Join(r.dir, JournalName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Close compacts and releases the journal. A roster that was opened and never
// written still compacts, which is how a roster seeded from the embedded list
// survives its first run.
func (r *Roster) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.compact()
	if r.journal != nil {
		if cerr := r.journal.Close(); err == nil {
			err = cerr
		}
		r.journal, r.buf = nil, nil
	}
	return err
}

// Reopen puts the journal back after a compaction, so a long sweep can compact
// midway and keep going.
func (r *Roster) Reopen() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buf != nil {
		return nil
	}
	return r.openJournal()
}

// writeZstdAtomic compresses b and renames it into place, so that a reader sees
// the previous roster or this one and never half of one.
//
// The bytes are synced before the rename. The directory deliberately is not: a
// rename that does not survive a power cut leaves the previous roster beside
// the journal that was about to be dropped, and that pair replays to exactly
// what this call was trying to write. Buying durability for the rename
// separately would only make that recovery happen less often, not make it
// unnecessary - the same reasoning store's writeFileAtomic gives.
func writeZstdAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err := enc.Write(b); err != nil {
		_ = enc.Close()
		_ = f.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
