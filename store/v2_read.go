package store

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha/oai"
)

// ErrNotHarvested is returned by a read of an identity the cache holds nothing
// for. Answering with silence would read as an endpoint that has no records,
// which is the one answer that is both wrong and plausible.
var ErrNotHarvested = errors.New("store: nothing harvested")

// Records streams the group's records.
//
// The index is asked first which windows can hold a matching record, and only
// the bytes those windows appended are decompressed: on a shard whose coverage
// spans a decade, a query about last month touches what last month's commits
// wrote instead of all of it.
//
// Going through the index even when nothing is filtered is what makes a read a
// view rather than a dump. A window that was fetched again - because it reached
// into the endpoint's present, or because it had no boundaries at all - leaves
// its older bytes in the segment, since the blob layer is append-only, but the
// extent naming them is gone, and a run of bytes no extent names is one no read
// can reach.
//
// The index prunes; it never decides. Every record that comes out of an extent
// is still checked against the filter, so an index that has fallen behind the
// segments can cost time but cannot produce a wrong answer.
func (s *v2Store) Records(opts ReadOptions) iter.Seq2[oai.Record, error] {
	return func(yield func(oai.Record, error) bool) {
		// Whether the endpoint was harvested at all is meta.json's answer, not
		// the index's: the shard's account of which groups it holds is written
		// when the group is first opened, where the index is written by the
		// first commit. So a harvest that got as far as an empty window reads
		// as empty, and one that was never run reads as never run.
		if !hasGroup(s.baseDir, s.id) {
			yield(oai.Record{}, fmt.Errorf("%v: %w", s.id, ErrNotHarvested))
			return
		}
		st, err := loadState(filepath.Join(s.Dir(), stateName))
		if err != nil {
			yield(oai.Record{}, err)
			return
		}
		g := st.group(s.id.Format, s.id.Set)
		if g == nil {
			return
		}
		for _, e := range g.liveExtents(opts) {
			path := filepath.Join(s.segDir(), segFileName(e.Seg))
			if !recordsFromExtent(path, e, opts, yield) {
				return
			}
		}
	}
}

// recordsFromExtent yields the matching records of one run of bytes. It returns
// false when iteration should stop, either because the consumer asked for it or
// because the bytes could not be read, in which case the error was yielded.
//
// The extent is decoded as a stream rather than in one piece: it is a whole
// number of frames, and a month-wide window can hold a great many of them.
func recordsFromExtent(path string, e extent, opts ReadOptions, yield func(oai.Record, error) bool) bool {
	f, err := os.Open(path)
	if err != nil {
		yield(oai.Record{}, err)
		return false
	}
	defer f.Close()
	dec, err := zstd.NewReader(io.NewSectionReader(f, e.Off, e.Len))
	if err != nil {
		yield(oai.Record{}, fmt.Errorf("%s: %w", path, err))
		return false
	}
	defer dec.Close()
	// Each frame holds several responses back to back, and zstd reads a run of
	// frames as one stream, so the whole extent decodes as a sequence of
	// documents.
	xd := xml.NewDecoder(dec)
	xd.Strict = false
	for {
		var resp oai.Response
		if err := xd.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				return true
			}
			yield(oai.Record{}, fmt.Errorf("failed to decode XML from %s: %w", path, err))
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
