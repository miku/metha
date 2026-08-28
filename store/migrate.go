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

	"github.com/miku/metha"
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
	Present  int      // records the shard holds for the source's windows
	Diverged []string // windows where the two disagree, by end date
	Skipped  []string // files whose name carries no window date
}

// Verified reports whether the shard holds every record the v1 directory does.
// It is checked window by window: the ones this run wrote match by
// construction, and the ones that were already there are counted again from the
// source and compared against the index, so a second run verifies exactly as
// strictly as the first. Comparing per window rather than in total is what lets
// a shard that has since been harvested further still verify - those windows
// are not the source's, and are not counted here.
//
// A migration that does not verify must not be followed by removing the source.
func (r *MigrateResult) Verified() bool {
	return len(r.Diverged) == 0 && r.Source == r.Present
}

// Migrate builds a v2 shard from an endpoint's v1 directory, without touching
// the network: a v1 file holds complete responses, so everything v2 needs is
// already on disk. It is safe to re-run - windows already present are skipped -
// and it leaves the v1 directory alone; removing it is the caller's decision,
// and only sensible once the result verifies.
func Migrate(baseDir string, id Identity) (*MigrateResult, error) {
	src := &v1Store{baseDir: baseDir, id: id}
	files, err := src.dataFiles()
	if err != nil {
		return nil, err
	}
	// Hold the v1 lock too, so a harvest cannot be finalizing files into the
	// directory that is being read.
	lock, err := metha.TryFlock(filepath.Join(src.Dir(), metha.LockName))
	if err != nil {
		return nil, err
	}
	if lock != nil {
		defer lock.Close()
	}
	result := &MigrateResult{Identity: id}
	byDate := map[string][]string{}
	for _, file := range files {
		groups := v1FilePattern.FindStringSubmatch(filepath.Base(file))
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

	w, err := OpenWriter(baseDir, id)
	if err != nil {
		return nil, err
	}
	defer w.Close()

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
		has, err := w.HasWindow(from, until)
		if err != nil {
			return nil, err
		}
		if has {
			// Already migrated. Count the source again and check it against
			// the index, rather than assume an earlier run got it right: this
			// is the only evidence there is that the v1 files can go.
			source, err := countV1Records(byDate[date])
			if err != nil {
				return nil, err
			}
			present, err := w.WindowRecords(from, until)
			if err != nil {
				return nil, err
			}
			result.Source += source
			result.Present += present
			if source != present {
				result.Diverged = append(result.Diverged, date)
			}
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
		// construction; both counts move together.
		result.Appended += w.Records()
		result.Source += w.Records()
		result.Present += w.Records()
		if err := w.Commit(); err != nil {
			return nil, err
		}
		result.Windows++
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
// may hold several: metha-pack concatenates them. The bytes are passed through
// untouched rather than decoded and re-encoded, so a migrated shard holds
// exactly what the endpoint sent.
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
		var resp metha.Response
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
