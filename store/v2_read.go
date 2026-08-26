package store

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"path/filepath"

	"github.com/miku/metha"
)

// segFrame is one frame of one segment, ready to be read.
type segFrame struct {
	Path  string
	Frame frame
}

// Records streams the group's records.
//
// Without a filter, that means walking the segments in order: the segments are
// the source of truth and reading them whole is the right shape for "give me
// everything". With a filter, the index is asked first which frames can hold a
// matching record, and only those are decompressed - on a shard whose windows
// span a decade, a query about last month then touches a handful of megabytes
// instead of all of them.
//
// The index prunes; it never decides. Every record that comes out of a frame is
// still checked against the filter, so an index that has fallen behind the
// segments can cost time but cannot produce a wrong answer.
func (s *v2Store) Records(opts ReadOptions) iter.Seq2[metha.Record, error] {
	if !opts.selective() {
		return s.recordsByScan(opts)
	}
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
	if opts.From != "" {
		query += ` AND records.datestamp >= ?`
		args = append(args, opts.From)
	}
	if opts.Until != "" {
		query += ` AND records.datestamp <= ?`
		args = append(args, opts.Until)
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
