package store

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"path/filepath"
	"time"

	"github.com/miku/metha"
)

// dayOf returns the date a bound falls on, or the empty string if there is no
// bound or it does not start with one. Every datestamp of that day, in either
// granularity, sorts at or after it.
func dayOf(bound string) string {
	if len(bound) < dayLen {
		return ""
	}
	return bound[:dayLen]
}

// dayAfter returns the day following a bound's, which is the first string every
// datestamp of that day sorts before - the upper end of dayOf's half-open
// range. An unparseable bound yields the empty string, and so prunes nothing:
// a filter the index cannot help with is answered by reading more, never by
// reading less.
func dayAfter(bound string) string {
	day := dayOf(bound)
	if day == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

// segFrame is one frame of one segment, ready to be read.
type segFrame struct {
	Path  string
	Frame frame
}

// Records streams the group's records.
//
// The index is asked first which frames can hold a matching record, and only
// those are decompressed: on a shard whose windows span a decade, a query about
// last month touches a handful of megabytes instead of all of them.
//
// Going through the index even when nothing is filtered is what makes a read a
// view rather than a dump. A window that was fetched again - because it reached
// into the endpoint's present, or because it had no boundaries at all - leaves
// its older bytes in the segment, since the blob layer is append-only, but its
// older rows are gone from the index. Frames never span two windows, because
// Commit always flushes one before writing the row, so a superseded window's
// frames are named by nothing and never read.
//
// The index prunes; it never decides. Every record that comes out of a frame is
// still checked against the filter, so an index that has fallen behind the
// segments can cost time but cannot produce a wrong answer. An index that is
// missing altogether falls back to reading the segments whole, duplicates and
// all: the segments are the cache, and answering from them beats not answering.
func (s *v2Store) Records(opts ReadOptions) iter.Seq2[metha.Record, error] {
	return func(yield func(metha.Record, error) bool) {
		frames, err := s.matchingFrames(opts)
		if err != nil {
			// An unreadable or missing index is not a reason to answer
			// wrongly: fall back to reading everything.
			if !errors.Is(err, errNoIndex) {
				yield(metha.Record{}, err)
				return
			}
			s.recordsByScan(opts)(yield)
			return
		}
		for _, sf := range frames {
			content, err := readFrame(sf.Path, sf.Frame)
			if err != nil {
				yield(metha.Record{}, fmt.Errorf("%s: %w", sf.Path, err))
				return
			}
			if !recordsFromBytes(content, opts, yield) {
				return
			}
		}
	}
}

// recordsByScan reads every segment of the group, in write order.
func (s *v2Store) recordsByScan(opts ReadOptions) iter.Seq2[metha.Record, error] {
	return func(yield func(metha.Record, error) bool) {
		files, err := s.segments()
		if err != nil {
			yield(metha.Record{}, err)
			return
		}
		for _, file := range files {
			if !recordsFromFile(file, opts, yield) {
				return
			}
		}
	}
}

// errNoIndex marks a group the index cannot answer for, which sends a read back
// to the segments rather than reporting a failure.
var errNoIndex = errors.New("store: no index for this group")

// matchingFrames asks the index which frames can hold a matching record.
//
// Only the datestamp bounds and the deleted status narrow the query. A record
// can carry several setSpecs and the index keeps them as one field, so using it
// to exclude a frame could drop a match; that filter is left to the records
// themselves, where it is exact.
func (s *v2Store) matchingFrames(opts ReadOptions) ([]segFrame, error) {
	if !isShard(s.Dir()) {
		return nil, errNoIndex
	}
	st, err := openState(filepath.Join(s.Dir(), stateName))
	if err != nil {
		return nil, err
	}
	defer st.close()
	groupID, err := st.groupID(s.id.Format, s.id.Set)
	if err != nil {
		return nil, err
	}
	if groupID == 0 {
		return nil, errNoIndex
	}
	query := `
		SELECT DISTINCT segments.name, records.frame_off, records.frame_len
		FROM records
		JOIN windows ON records.window_id = windows.id
		JOIN segments ON records.seg = segments.id
		WHERE windows.group_id = ?`
	args := []any{groupID}
	// Pruned at day resolution, not at the bound's own. The column holds what
	// the endpoint sent, in whichever granularity it uses, and the two forms do
	// not compare cleanly as text - so the bounds are rounded outwards to whole
	// days, where every form of a datestamp sorts where it should. That keeps
	// the comparison sargable against records_datestamp, keeps the prune worth
	// having, and cannot drop a frame that holds a match; opts.match still
	// decides to the second.
	if day := dayOf(opts.From); day != "" {
		query += ` AND records.datestamp >= ?`
		args = append(args, day)
	}
	if day := dayAfter(opts.Until); day != "" {
		query += ` AND records.datestamp < ?`
		args = append(args, day)
	}
	switch opts.Deleted {
	case DeletedSkip:
		query += ` AND records.status != 'deleted'`
	case DeletedOnly:
		query += ` AND records.status = 'deleted'`
	}
	query += ` ORDER BY records.seg, records.frame_off`
	rows, err := st.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var frames []segFrame
	for rows.Next() {
		var (
			name string
			fr   frame
		)
		if err := rows.Scan(&name, &fr.Off, &fr.Len); err != nil {
			return nil, err
		}
		frames = append(frames, segFrame{Path: filepath.Join(s.segDir(), name), Frame: fr})
	}
	return frames, rows.Err()
}

// recordsFromBytes yields the matching records of one decompressed frame.
func recordsFromBytes(content []byte, opts ReadOptions, yield func(metha.Record, error) bool) bool {
	dec := xml.NewDecoder(bytes.NewReader(content))
	dec.Strict = false
	for {
		var resp metha.Response
		if err := dec.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				return true
			}
			yield(metha.Record{}, fmt.Errorf("failed to decode XML in frame: %w", err))
			return false
		}
		for _, rec := range resp.ListRecords.Records {
			if !opts.match(&rec) {
				continue
			}
			if !yield(rec, nil) {
				return false
			}
		}
	}
}
