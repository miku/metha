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
}

// Unknown marks a count the layout cannot answer.
const Unknown = -1

// Stat summarises one harvested identity.
func Stat(baseDir string, id Identity) (*Stats, error) {
	s, err := Open(baseDir, id)
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
		return stats, nil
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
		first, last, started, finished sql.NullString
		empty, failed                  sql.NullInt64
		windows, requests, fetched     sql.NullInt64
	)
	err = st.db.QueryRow(`
		SELECT COUNT(*), MIN(from_ts), MAX(until_ts), SUM(requests), SUM(bytes),
		       SUM(status = ?), SUM(status = ?), MIN(started), MAX(finished)
		FROM windows WHERE group_id = ?`,
		statusEmpty, statusError, groupID).
		Scan(&windows, &first, &last, &requests, &fetched, &empty, &failed, &started, &finished)
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
	// Time spent harvesting, window by window, rather than the wall clock
	// between the first and the last: a shard is usually filled in over many
	// runs, days apart.
	rows, err := st.db.Query(`SELECT started, finished FROM windows WHERE group_id = ? AND started != '' AND finished != ''`, groupID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return err
		}
		start, err1 := time.Parse(time.RFC3339, from)
		end, err2 := time.Parse(time.RFC3339, to)
		if err1 == nil && err2 == nil && end.After(start) {
			stats.Elapsed += end.Sub(start)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
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
