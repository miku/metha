package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/miku/metha"
)

// V2 is the sharded layout: one directory per base URL, holding an append-only
// zstd segment file per (format, set) group, a sqlite index and a human
// readable description of the whole shard.
const V2 Layout = "v2"

const (
	// metaName describes a shard well enough to rebuild its index.
	metaName = "meta.json"
	// stateName is the per-shard sqlite index: windows, segments, records.
	stateName = "state.sqlite"
	// segDirname holds one subdirectory of segments per group, so that
	// "cat seg/<group>/*.zst | zstd -dc" is a complete, valid stream.
	segDirname = "seg"
)

// Meta is the content of a shard's meta.json. It is the shard's own account of
// what it holds: the index and every export are derived from the segments and
// can be rebuilt, but this file says which endpoint the bytes came from.
type Meta struct {
	Layout   Layout          `json:"layout"`
	BaseURL  string          `json:"base_url"`
	Created  time.Time       `json:"created"`
	Updated  time.Time       `json:"updated"`
	Identify *metha.Identify `json:"identify,omitempty"`
	Groups   []Group         `json:"groups"`
}

// Group is one (format, set) pair harvested from an endpoint, and the
// directory its segments live in.
type Group struct {
	Format string `json:"format"`
	Set    string `json:"set,omitempty"`
	Dir    string `json:"dir"`
}

// group returns the group of an identity, with the directory name it maps to.
func (id Identity) group() Group {
	return Group{Format: id.Format, Set: id.Set, Dir: groupName(id.Format, id.Set)}
}

// shardDir returns the shard directory for a base URL. Hashing the URL fixes
// two problems the v1 names had: a base64 endpoint name can exceed the length
// a filesystem allows, and a cache of every known endpoint puts a quarter of a
// million entries in a single directory.
// Shards sit directly in the cache, with no directory naming the layout above
// them. The two fan-out levels are two hex digits each, and a v1 endpoint
// directory is base64 of "set#format#baseURL", which for any identity that can
// actually be harvested is at least four characters - so the two cannot be
// confused while a migration has both in the same cache. Not having a
// directory to name is also the only way of never having to rename it: after
// 1.0 the module itself gets a /v2 path, and there is no cache path left to
// make "v2" ambiguous in.
func shardDir(baseDir, baseURL string) string {
	sum := sha256.Sum256([]byte(baseURL))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(baseDir, h[0:2], h[2:4], h[:16])
}

// isShardPrefix reports whether a cache entry is one of the fan-out levels: two
// lowercase hex digits, which is what keeps a listing from descending into
// every v1 endpoint directory looking for shards.
func isShardPrefix(name string) bool {
	if len(name) != 2 {
		return false
	}
	for i := range 2 {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// groupName renders a (format, set) pair as one directory name. It stays
// readable in a listing, which is the point of grouping segments by format and
// set at all, and a digest is appended whenever the readable form had to drop
// characters, so that two sets differing only in punctuation cannot collide.
func groupName(format, set string) string {
	name := sanitize(format)
	if set != "" {
		name += "+" + sanitize(set)
	}
	if safeComponent(name) && name == plainGroupName(format, set) && len(name) <= 64 {
		return name
	}
	if len(name) > 64 {
		name = name[:64]
	}
	sum := sha256.Sum256([]byte(format + "\x00" + set))
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}

// plainGroupName is the group name as it would read if nothing needed escaping.
// safeComponent reports whether a name can stand as a directory of its own.
// sanitize keeps dots, because formats and sets are full of them, and three
// names survive it that a filesystem has already spoken for: the empty string,
// which joins to nothing and would put a group's segments in seg/ itself, and
// "." and "..", which are directories that already exist - a format of ".."
// would write segments into the shard root, beside meta.json and the index.
// They fall through to the digest instead, which is where every other name
// that cannot be spelled plainly already goes.
func safeComponent(name string) bool {
	return name != "" && name != "." && name != ".."
}

func plainGroupName(format, set string) string {
	if set == "" {
		return format
	}
	return format + "+" + set
}

// sanitize keeps the characters that are unremarkable in a path on every
// platform metha builds for, and replaces the rest.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		}
		return '-'
	}, s)
}

// readMeta loads a shard's meta.json.
func readMeta(shard string) (*Meta, error) {
	b, err := os.ReadFile(filepath.Join(shard, metaName))
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(shard, metaName), err)
	}
	return &m, nil
}

// writeMeta writes a shard's meta.json into place atomically, so that a crash
// cannot leave a shard that cannot say what it is.
func writeMeta(shard string, m *Meta) error {
	m.Updated = time.Now().UTC()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := filepath.Join(shard, metaName+".tmp")
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(shard, metaName))
}

// hasGroup reports whether the meta already knows this group.
func (m *Meta) hasGroup(g Group) bool {
	for _, have := range m.Groups {
		if have.Format == g.Format && have.Set == g.Set {
			return true
		}
	}
	return false
}

// removeGroup drops a group from the meta, so that a listing stops announcing
// data the shard no longer holds.
func (m *Meta) removeGroup(g Group) {
	kept := m.Groups[:0]
	for _, have := range m.Groups {
		if have.Format != g.Format || have.Set != g.Set {
			kept = append(kept, have)
		}
	}
	m.Groups = kept
}

// isShard reports whether dir is a v2 shard.
func isShard(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, metaName))
	return err == nil
}

// v2Store reads one group of one shard. Reads go straight to the segments, in
// order, which keeps them independent of the index; the index is what phase 3
// will use to skip frames that cannot match a filter.
type v2Store struct {
	baseDir string
	id      Identity
}

func (s *v2Store) Identity() Identity { return s.id }

func (s *v2Store) Layout() Layout { return V2 }

// Dir returns the shard directory, which holds every group of this endpoint.
// The files of this particular group are under seg/<group>.
func (s *v2Store) Dir() string { return shardDir(s.baseDir, s.id.BaseURL) }

// segDir is where this group's segments live.
func (s *v2Store) segDir() string {
	return filepath.Join(s.Dir(), segDirname, groupName(s.id.Format, s.id.Set))
}

// Files returns the group's segments in write order. Concatenating them yields
// a valid zstd stream, so "metha-files ... | xargs cat | zstd -dc" prints every
// response this group ever harvested.
func (s *v2Store) Files() ([]string, error) {
	files, err := s.segments()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return files, err
}

// segments lists the group's segment files, reporting a group that was never
// harvested as the missing directory it is.
func (s *v2Store) segments() ([]string, error) {
	entries, err := os.ReadDir(s.segDir())
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), segExt) {
			files = append(files, filepath.Join(s.segDir(), entry.Name()))
		}
	}
	return files, nil
}

// Last returns the end of the most recent harvested window, from the index.
func (s *v2Store) Last() (string, error) {
	if !isShard(s.Dir()) {
		return "", nil
	}
	st, err := openState(filepath.Join(s.Dir(), stateName))
	if err != nil {
		return "", err
	}
	defer st.close()
	groupID, err := st.groupID(s.id.Format, s.id.Set)
	if err != nil || groupID == 0 {
		return "", err
	}
	last, err := st.lastWindow(groupID)
	if err != nil {
		return "", err
	}
	// Windows are stored as timestamps; callers resuming a harvest speak
	// dates, as they did in v1.
	return windowDate(last)
}

// listV2 enumerates the shards under baseDir, one entry per group. A shard is
// found by its meta.json, which also says which groups it holds, so a listing
// never has to open an index.
func listV2(baseDir string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		// The tree is exactly $METHA_DIR/<aa>/<bb>/<hash>, so a bounded walk
		// beats filepath.WalkDir over a cache of a quarter million shards.
		// Only the fan-out names are descended into, which is what keeps a
		// cache that still holds v1 directories from being walked twice.
		for _, aa := range subdirs(baseDir) {
			if !isShardPrefix(aa) {
				continue
			}
			for _, bb := range subdirs(filepath.Join(baseDir, aa)) {
				for _, shard := range subdirs(filepath.Join(baseDir, aa, bb)) {
					dir := filepath.Join(baseDir, aa, bb, shard)
					if !isShard(dir) {
						continue
					}
					meta, err := readMeta(dir)
					if err != nil {
						if !yield(Entry{}, err) {
							return
						}
						continue
					}
					for _, g := range meta.Groups {
						e := Entry{
							Identity: Identity{BaseURL: meta.BaseURL, Format: g.Format, Set: g.Set},
							Layout:   V2,
							Dir:      dir,
						}
						if !yield(e, nil) {
							return
						}
					}
				}
			}
		}
	}
}

// subdirs returns the names of the subdirectories of dir, or nothing if it
// does not exist.
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}
