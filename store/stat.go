package store

import (
	"os"
	"time"
)

// Stats is what a store can say about itself without decompressing anything.
// Every count comes from the index, which is why a shard can answer at all: the
// pre-1.0 layout kept no account of what it held beyond its filenames.
type Stats struct {
	Identity Identity

	Files int   // segments
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

// Stat summarises one harvested identity.
//
// An identity the cache holds nothing for is ErrNotHarvested rather than a
// report of zeroes, which is the same answer Records gives and for the same
// reason: a full report saying nothing was harvested, nothing failed and nothing
// is on disk is indistinguishable from an endpoint that answered with nothing,
// and a mistyped URL or the wrong -format is by far the likelier of the two.
func Stat(baseDir string, id Identity) (*Stats, error) {
	s, err := Open(baseDir, id)
	if err != nil {
		return nil, err
	}
	if !hasGroup(baseDir, id) {
		return nil, notHarvested(id)
	}
	stats := &Stats{Identity: id}
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
	if err := statIndex(s.Dir(), id, stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// statIndex fills in what only the index knows.
func statIndex(shard string, id Identity, stats *Stats) error {
	st, err := loadState(statePath(shard, id.Format, id.Set), id.Format, id.Set)
	if err != nil {
		return err
	}
	var first time.Time
	for i, w := range st.Windows {
		stats.Windows++
		switch w.Status {
		case statusEmpty:
			stats.Empty++
		case statusError:
			stats.Failed++
		}
		stats.Requests += w.Requests
		stats.Records += w.Records
		stats.Deleted += w.Deleted
		stats.Fetched += w.Bytes
		// Time spent harvesting, added up as each window was committed, rather
		// than the wall clock between the first and the last: a shard is filled
		// in over many runs, days apart, and settled windows merge, so a single
		// row's started and finished can be a year apart with a minute's work
		// between.
		stats.Elapsed += time.Duration(w.Elapsed)
		if i == 0 || w.From.Before(first) {
			first = w.From
		}
		if w.Finished.After(stats.LastSeen) {
			stats.LastSeen = w.Finished
		}
	}
	stats.First = windowDate(first)
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
