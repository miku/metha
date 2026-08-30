package store

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/miku/metha/oai"
)

// MigrateResult reports what a migration moved, and what it could not.
type MigrateResult struct {
	Identity Identity
	Windows  int      // windows written by this run
	Requests int      // responses read by this run
	Appended int      // records written by this run
	Bytes    int64    // uncompressed response bytes written
	Records  int      // records the shard holds for this group afterwards
	Source   int      // records the v1 directory holds
	Present  int      // records the shard holds over the range the source covers
	Skipped  []string // files whose name carries no window date
}

// Verified reports whether the shard holds every record the v1 directory does.
// Both sides are counted the same way - the source by rereading it, the shard
// by counting index rows - so a second run verifies exactly as strictly as the
// first, rather than trusting that an earlier one got it right.
//
// The comparison spans the whole range the source covers, because that is the
// shape the index has: settled windows are merged as they are committed, so a
// migrated cache answers out of one row and there is no per-day count left to
// compare. A shard that has been harvested past the source's range reports
// short and so refuses to verify, which is the safe way round for something
// whose only use is to gate removing the original.
//
// A migration that does not verify must not be followed by removing the source.
func (r *MigrateResult) Verified() bool {
	return r.Source == r.Present
}

// Migrate builds a shard from an endpoint's pre-1.0 directory, without touching
// the network: those files hold complete responses, so everything the shard
// needs is already on disk. It is safe to re-run - windows already present are
// skipped - and it leaves the source alone; removing it is the caller's
// decision, and only sensible once the result verifies.
func Migrate(baseDir string, id Identity) (*MigrateResult, error) {
	files, err := legacyFiles(baseDir, id)
	if err != nil {
		return nil, err
	}
	// Hold the source directory's lock too. No 1.0 harvest writes there, but a
	// 0.5.x one still can, and a half-renamed file must not be read as data.
	lock, err := TryFlock(filepath.Join(legacyDir(baseDir, id), LockName))
	if err != nil {
		return nil, err
	}
	if lock != nil {
		defer lock.Close()
	}
	result := &MigrateResult{Identity: id}
	byDate := map[string][]string{}
	for _, file := range files {
		groups := legacyFilePattern.FindStringSubmatch(filepath.Base(file))
		if len(groups) < 2 {
			result.Skipped = append(result.Skipped, file)
			continue
		}
		byDate[groups[1]] = append(byDate[groups[1]], file)
	}
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	// openWriter rather than OpenWriter: the refusal exists to keep every other
	// command away from an unconverted endpoint, and this is the command that
	// converts it.
	w, err := openWriter(baseDir, id)
	if err != nil {
		return nil, err
	}
	defer w.Close()

	// The range the source covers, accumulated as the windows are worked out,
	// and what the shard is counted over at the end.
	var spanFrom, spanUntil time.Time
	for i, date := range dates {
		// The date in a v1 filename stands for the whole of that day, which is
		// how the endpoint read the request that produced it, and it is a local
		// date because that is the zone the harvester works in. Spelling both
		// out here is what lets every window boundary in the shard be exact.
		day, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err != nil {
			return nil, err
		}
		until := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
		// v1 harvests contiguous ranges and only records where each one
		// ended, so a window starts the day after the previous one. The first
		// window is the exception: how far back it reached is not recorded
		// anywhere, so it claims only the day it ended on.
		from := day
		if i > 0 {
			prev, err := time.ParseInLocation("2006-01-02", dates[i-1], time.Local)
			if err != nil {
				return nil, err
			}
			from = prev.AddDate(0, 0, 1)
		}
		if spanFrom.IsZero() {
			spanFrom = from
		}
		spanUntil = until
		has, err := w.HasWindow(from, until)
		if err != nil {
			return nil, err
		}
		if has {
			// Already migrated. Count the source again anyway, rather than
			// assume an earlier run got it right: this is the only evidence
			// there is that the v1 files can go.
			source, err := countV1Records(byDate[date])
			if err != nil {
				return nil, err
			}
			result.Source += source
			continue
		}
		// Settled by construction: v1 harvests never reached past the end of
		// the day before they ran, so every window they left behind is final.
		if err := w.Begin(from, until, true); err != nil {
			return nil, err
		}
		if err := migrateWindow(w, byDate[date], result); err != nil {
			return nil, errors.Join(err, w.Abort(err))
		}
		// Written here, so the shard holds what the source did by
		// construction; the count is still taken from the index below.
		result.Appended += w.Records()
		result.Source += w.Records()
		if err := w.Commit(); err != nil {
			return nil, err
		}
		result.Windows++
	}
	if !spanFrom.IsZero() {
		if result.Present, err = w.WindowRecords(spanFrom, spanUntil); err != nil {
			return nil, err
		}
	}
	if result.Records, err = w.CountRecords(); err != nil {
		return nil, err
	}
	return result, nil
}

// migrateWindow appends every response of a day's files to the open window.
func migrateWindow(w *Writer, files []string, result *MigrateResult) error {
	for _, file := range files {
		raws, err := rawResponses(file)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		for _, raw := range raws {
			if err := w.Append(raw); err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			result.Requests++
			result.Bytes += int64(len(raw))
		}
	}
	return nil
}

// countV1Records counts the records in a window's v1 files, by the same scan
// that indexes them on the way in, so that the two numbers being compared were
// produced the same way.
func countV1Records(files []string) (int, error) {
	var n int
	for _, file := range files {
		raws, err := rawResponses(file)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", file, err)
		}
		for _, raw := range raws {
			refs, err := scanRecords(raw)
			if err != nil {
				return 0, fmt.Errorf("%s: %w", file, err)
			}
			n += len(refs)
		}
	}
	return n, nil
}

// rawResponses returns the response documents of a v1 file, as bytes. A file
// may hold several, in a cache the old metha-pack command was run on. The bytes
// are passed through untouched rather than decoded and re-encoded, so a migrated
// shard holds exactly what the endpoint sent.
func rawResponses(path string) ([][]byte, error) {
	data, err := readWhole(path)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	var out [][]byte
	for {
		prev := dec.InputOffset()
		var resp oai.Response
		if err := dec.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, err
		}
		end := dec.InputOffset()
		start := prev + int64(bytes.IndexByte(data[prev:end], '<'))
		out = append(out, data[start:end])
	}
}

// readWhole reads a data file, decompressing it if its name says to.
func readWhole(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := decompress(path, f)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// CountRecords returns how many records the shard has indexed for this group,
// which is what a migration checks its source against.
func (w *Writer) CountRecords() (int, error) { return w.st.countRecords(w.groupID) }
