package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Stats is what a store can say about itself without decompressing anything.
//
// The counts come from the index, so they are v2 only: v1 keeps no record of
// what it holds beyond the filenames, and answering "how many records" there
// means reading every file. Fields a layout cannot answer are left at Unknown.
type Stats struct {
	Identity Identity
	Layout   Layout

	Files int   // data files, or segments
	Bytes int64 // bytes on disk

	Windows  int // ranges harvested
	Empty    int // of those, the ones that returned nothing
	Failed   int
	Requests int
	Records  int
	Deleted  int

	First    string        // start of the earliest window
	Last     string        // end of the latest
	Elapsed  time.Duration // time spent harvesting, summed over windows
	Fetched  int64         // uncompressed response bytes, as harvested
	LastSeen time.Time     // when the most recent window finished

	// Superseded marks a v1 directory whose format and set now also live in a
	// v2 shard: a leftover copy, since metha-migrate keeps the source unless
	// -rm is given. Its bytes are still on disk and still counted here, but
	// nothing reads them any more.
	Superseded bool
	// StaleV1 names that leftover directory when looking from the v2 side.
	StaleV1 string
}

// Unknown marks a count the layout cannot answer.
const Unknown = -1

// Stat summarises one harvested identity, in whichever layout holds it.
func Stat(baseDir string, id Identity) (*Stats, error) {
	return StatLayout(baseDir, id, "")
}

// StatLayout is Stat with the layout forced. An empty layout means detect.
// Callers walking List pass the layout of the entry they were handed: while a
// migration is under way the same identity can exist in both layouts, and
// detection deliberately reports only the one that would be read.
func StatLayout(baseDir string, id Identity, layout Layout) (*Stats, error) {
	s, err := OpenLayout(baseDir, id, layout)
	if err != nil {
		return nil, err
	}
	stats := &Stats{Identity: id, Layout: s.Layout()}
	files, err := s.Files()
	if err != nil {
		return nil, err
	}
	stats.Files = len(files)
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		stats.Bytes += info.Size()
	}
	if stats.Last, err = s.Last(); err != nil {
		return nil, err
	}
	if s.Layout() != V2 {
		stats.Windows, stats.Empty, stats.Failed = Unknown, Unknown, Unknown
		stats.Requests, stats.Records, stats.Deleted = Unknown, Unknown, Unknown
		stats.Fetched = Unknown
		stats.Superseded = hasV2Group(baseDir, id)
		return stats, nil
	}
	if v1 := v1Dir(baseDir, id); isDir(v1) {
		stats.StaleV1 = v1
	}
	if err := statIndex(s.Dir(), id, stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// statIndex fills in what only the index knows.
func statIndex(shard string, id Identity, stats *Stats) error {
	st, err := openState(filepath.Join(shard, stateName))
	if err != nil {
		return err
	}
	defer st.close()
	groupID, err := st.groupID(id.Format, id.Set)
	if err != nil || groupID == 0 {
		return err
	}
	var (
		first, last, finished      sql.NullString
		empty, failed, elapsed     sql.NullInt64
		windows, requests, fetched sql.NullInt64
	)
	err = st.db.QueryRow(`
		SELECT COUNT(*), MIN(from_ts), MAX(until_ts), SUM(requests), SUM(bytes),
		       SUM(status = ?), SUM(status = ?), MAX(finished), SUM(elapsed_ns)
		FROM windows WHERE group_id = ?`,
		statusEmpty, statusError, groupID).
		Scan(&windows, &first, &last, &requests, &fetched, &empty, &failed, &finished, &elapsed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	stats.Windows = int(windows.Int64)
	stats.Empty = int(empty.Int64)
	stats.Failed = int(failed.Int64)
	stats.Requests = int(requests.Int64)
	stats.Fetched = fetched.Int64
	if first.Valid {
		stats.First, _ = windowDate(first.String)
	}
	if finished.Valid {
		stats.LastSeen, _ = time.Parse(time.RFC3339, finished.String)
	}
	// Time spent harvesting, added up as each window was committed, rather than
	// the wall clock between the first and the last: a shard is filled in over
	// many runs, days apart, and settled windows merge, so a single row's
	// started and finished can be a year apart with a minute's work between.
	stats.Elapsed = time.Duration(elapsed.Int64)
	var records, deleted sql.NullInt64
	if err := st.db.QueryRow(`
		SELECT COUNT(*), SUM(records.status = 'deleted')
		FROM records JOIN windows ON records.window_id = windows.id
		WHERE windows.group_id = ?`, groupID).Scan(&records, &deleted); err != nil {
		return err
	}
	stats.Records = int(records.Int64)
	stats.Deleted = int(deleted.Int64)
	return nil
}

// Rate returns the harvesting throughput in uncompressed bytes per second, or
// zero if nothing was measured.
func (s *Stats) Rate() float64 {
	if s.Elapsed <= 0 || s.Fetched <= 0 {
		return 0
	}
	return float64(s.Fetched) / s.Elapsed.Seconds()
}

// Ratio returns how much smaller the stored bytes are than what was fetched.
func (s *Stats) Ratio() float64 {
	if s.Bytes <= 0 || s.Fetched <= 0 {
		return 0
	}
	return float64(s.Fetched) / float64(s.Bytes)
}
