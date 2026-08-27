package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// packOpts holds the pack flags, which the directory walk needs throughout.
type packOpts struct {
	baseDir     string
	minFiles    int
	verbose     bool
	dryRun      bool
	quietPeriod time.Duration
}

// newPackCmd iterates over all harvested files and compacts them per endpoint
// into a single file. This should improve read speeds for snapshotting.
//
// This is mostly LLM generated code, and not officially supported yet. The v2
// layout appends into segments that are packed by construction, so pack has
// nothing to do there and applies to v1 directories only.
func newPackCmd() *cobra.Command {
	var o packOpts
	cmd := &cobra.Command{
		Use:     "pack [DIR]",
		Short:   "Compact a v1 cache, one file per endpoint",
		Aliases: []string{"metha-pack"},
		RunE: func(cmd *cobra.Command, args []string) error {
			log.SetOutput(os.Stderr)
			root := o.baseDir
			if len(args) > 0 {
				root = args[0]
			}
			fmt.Fprintf(os.Stderr, "analyzing directory structure: %s\n", root)
			// Guard against two pack runs racing each other on the same tree.
			if !o.dryRun {
				lockFile, err := metha.TryFlock(filepath.Join(root, ".metha-pack.lock"))
				if err != nil {
					return fmt.Errorf("could not acquire pack lock: %w", err)
				}
				if lockFile != nil {
					defer lockFile.Close()
				}
			}
			stats := &packStats{}
			if o.dryRun {
				fmt.Fprintf(os.Stderr, "DRY RUN MODE - no files will be modified\n")
			}
			// Process directories in streaming fashion - only walk dirs, not
			// individual files.
			err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					// Ignore "no such file" errors - we may have deleted files
					// during processing.
					if os.IsNotExist(err) {
						return nil
					}
					return err
				}
				if !info.IsDir() || path == root {
					return nil
				}
				stats.TotalDirs++
				o.processDirectory(path, stats)
				return nil
			})
			if err != nil {
				return fmt.Errorf("error walking directories: %w", err)
			}
			fmt.Fprintf(os.Stderr, "directories processed: %d/%d\n", stats.ProcessedDirs, stats.TotalDirs)
			fmt.Fprintf(os.Stderr, "directories skipped: %d\n", stats.SkippedDirs)
			fmt.Fprintf(os.Stderr, "directories failed:  %d\n", stats.FailedDirs)
			fmt.Fprintf(os.Stderr, "files packed: %d\n", stats.PackedFiles)
			fmt.Fprintf(os.Stderr, "total files before: %d\n", stats.TotalFiles)
			if stats.BytesSaved > 0 {
				fmt.Fprintf(os.Stderr, "estimated metadata overhead saved: %.2f MB\n", float64(stats.BytesSaved)/(1024*1024))
			}
			if stats.FailedDirs > 0 {
				// Surface failures via exit code so CI/cron can detect them.
				os.Exit(1)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.baseDir, "base-dir", "d", metha.GetBaseDir(), "base directory for harvested files")
	f.IntVarP(&o.minFiles, "min-files", "m", 3, "minimum number of files before packing")
	// -v is verbose here, not version: that is what metha-pack has always
	// meant by it, and a local flag shadows the persistent one on the root.
	f.BoolVarP(&o.verbose, "verbose", "v", false, "verbose output")
	f.BoolVarP(&o.dryRun, "dry-run", "r", false, "show what would be done without actually doing it")
	f.DurationVar(&o.quietPeriod, "quiet", 60*time.Second, "skip endpoint dirs whose newest compressed file was modified within this window")
	return cmd
}

type packStats struct {
	TotalDirs     int
	ProcessedDirs int
	SkippedDirs   int
	FailedDirs    int
	TotalFiles    int
	PackedFiles   int
	BytesSaved    int64
}

// packExts lists the compressed file extensions pack handles. Files of
// different extensions must never be concatenated together: a .gz stream and a
// .zst stream packed into one file produce an unreadable hybrid.
var packExts = []string{".gz", ".zst"}

// staleSuffix marks originals that have been quarantined during a pack.
// Quarantined files are ignored by future pack runs because the suffix sits
// past the .gz/.zst extension, so compressedExt no longer matches. Visible
// .stale files after a run indicate something went wrong; they can be deleted
// once the user has verified the packed file.
const staleSuffix = ".stale"

// quarantineMove records a successful rename so we can roll it back.
type quarantineMove struct{ from, to string }

// compressedExt returns the matching extension from packExts, or "" if the
// file is not a packable compressed file.
func compressedExt(filename string) string {
	for _, ext := range packExts {
		if strings.HasSuffix(filename, ext) {
			return ext
		}
	}
	return ""
}

func (o *packOpts) processDirectory(path string, stats *packStats) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if o.verbose {
			log.Printf("warning: cannot read directory %s: %v", path, err)
		}
		stats.SkippedDirs++
		return
	}
	// Group by extension so we never mix .gz and .zst into one packed file.
	// Track the newest mtime so we can skip dirs that look like a harvest is
	// currently writing them.
	groups := make(map[string][]string, len(packExts))
	var newestMtime time.Time
	for _, e := range entries {
		if compressedExt(e.Name()) == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if o.verbose {
				log.Printf("warning: stat %s: %v", filepath.Join(path, e.Name()), err)
			}
			continue
		}
		if m := info.ModTime(); m.After(newestMtime) {
			newestMtime = m
		}
		ext := compressedExt(e.Name())
		groups[ext] = append(groups[ext], e.Name())
	}
	// Heuristic harvest-collision guard: if any compressed file was written
	// recently, a harvest may still be active; skip and let the next run
	// catch it.
	if !newestMtime.IsZero() && o.quietPeriod > 0 && time.Since(newestMtime) < o.quietPeriod {
		if o.verbose {
			log.Printf("skipped %s: newest file modified %s ago (< quiet period %s)",
				filepath.Base(path), time.Since(newestMtime).Round(time.Second), o.quietPeriod)
		}
		stats.SkippedDirs++
		return
	}
	if len(groups) == 0 {
		stats.SkippedDirs++
		return
	}
	// Coordinate with a running harvest.
	if !o.dryRun {
		lockFile, err := metha.TryFlock(filepath.Join(path, metha.LockName))
		if err != nil {
			if o.verbose {
				log.Printf("skipped %s: %v", filepath.Base(path), err)
			}
			stats.SkippedDirs++
			return
		}
		if lockFile != nil {
			defer lockFile.Close()
		}
	}
	// Pack each extension group independently.
	processed := false
	for _, ext := range packExts {
		files := groups[ext]
		if len(files) == 0 {
			continue
		}
		if o.packGroup(path, ext, files, stats) {
			processed = true
		}
	}
	if !processed {
		stats.SkippedDirs++
	}
}

// packGroup packs a single extension's files in one directory. Returns true
// when the group was packed (or would have been, in dry-run mode); false when
// it was skipped (too few files, concat failure, etc.).
//
// Pack ordering is designed so a crash never causes silent duplication on the
// next run:
//
//  1. concat all inputs to a tmp file (originals untouched)
//  2. quarantine all non-latest originals by renaming to <name>.stale
//  3. atomic rename tmp -> latestFile (the harvest offset signal)
//  4. best-effort rm of .stale files
//
// If phase 2 or 3 fails, we roll back any quarantine renames so the directory
// returns to its starting state. Leftover .stale files (or a leftover tmp)
// are visible signals of an incomplete prior run.
func (o *packOpts) packGroup(path, ext string, files []string, stats *packStats) bool {
	if len(files) < o.minFiles {
		if o.verbose {
			log.Printf("skipped %s (%s): only %d files (minimum: %d)",
				filepath.Base(path), ext, len(files), o.minFiles)
		}
		return false
	}
	sortFilesByDate(files)
	latestFile := files[len(files)-1]
	stats.TotalFiles += len(files)
	stats.PackedFiles += len(files)
	if o.dryRun {
		stats.ProcessedDirs++
		stats.BytesSaved += int64(len(files)-1) * 4096
		fmt.Printf("[%d] \033[90m%s\033[0m: ✓ packed %s %s files -> %s (DRY RUN)\n",
			stats.TotalDirs, filepath.Base(path), colorizeFileCount(len(files)), ext, latestFile)
		return true
	}
	targetPath := filepath.Join(path, latestFile)
	// Tmp path is per-extension so a same-dir group of the other extension
	// can't collide. Any leftover from a prior crashed run is overwritten
	// when we open it for writing.
	tmpPath := filepath.Join(path, ".tmp_pack"+ext)

	// Phase 1: concat all inputs into the tmp file. Originals untouched.
	if err := writeConcat(path, files, tmpPath); err != nil {
		log.Printf("error: concat for %s (%s) failed: %v", path, ext, err)
		os.Remove(tmpPath)
		stats.FailedDirs++
		return false
	}
	// Phase 2: quarantine non-latest originals so a partial-failure or crash
	// in phase 3+ can't cause future runs to re-include them and produce
	// duplicates. Renames are atomic on the same filesystem.
	var moves []quarantineMove
	for _, fn := range files {
		if fn == latestFile {
			continue
		}
		from := filepath.Join(path, fn)
		to := from + staleSuffix
		if err := os.Rename(from, to); err != nil {
			log.Printf("error: cannot quarantine %s: %v; rolling back", from, err)
			rollbackQuarantine(moves, stats)
			os.Remove(tmpPath)
			stats.FailedDirs++
			return false
		}
		moves = append(moves, quarantineMove{from, to})
	}
	// Phase 3: install the new pack atomically. Until this rename completes
	// the directory still contains the old latestFile (which is one of our
	// concat inputs and therefore fully represented in the tmp file).
	if err := os.Rename(tmpPath, targetPath); err != nil {
		log.Printf("error: cannot install pack file %s: %v; rolling back", targetPath, err)
		rollbackQuarantine(moves, stats)
		os.Remove(tmpPath)
		stats.FailedDirs++
		return false
	}
	// Phase 4: best-effort cleanup of quarantined files. Failures here don't
	// affect correctness - .stale files are skipped by future pack runs.
	deletedCount := 0
	for _, m := range moves {
		if err := os.Remove(m.to); err != nil {
			if o.verbose {
				log.Printf("warning: failed to delete %s: %v (left as orphan; safe to remove manually)", m.to, err)
			}
		} else {
			deletedCount++
		}
	}
	stats.ProcessedDirs++
	stats.BytesSaved += int64(deletedCount) * 4096
	fmt.Printf("[%d] \033[90m%s\033[0m: ✓ packed %s %s files, deleted %d files -> %s\n",
		stats.TotalDirs, filepath.Base(path), colorizeFileCount(len(files)), ext, deletedCount, latestFile)
	return true
}

// rollbackQuarantine reverses successful quarantine renames. A rollback
// failure leaves the directory in a half-quarantined state - uncommon (same
// filesystem, file we just created) but loud when it happens.
func rollbackQuarantine(moves []quarantineMove, stats *packStats) {
	for _, m := range moves {
		if err := os.Rename(m.to, m.from); err != nil {
			log.Printf("FATAL: rollback rename %s -> %s failed: %v; manual cleanup required", m.to, m.from, err)
			stats.FailedDirs++
		}
	}
}

func sortFilesByDate(files []string) {
	re := regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)
	sort.Slice(files, func(i, j int) bool {
		di := re.FindStringSubmatch(files[i])
		dj := re.FindStringSubmatch(files[j])
		if len(di) < 2 || len(dj) < 2 {
			return files[i] < files[j] // fallback to lexical sort
		}
		ti, _ := time.Parse("2006-01-02", di[1])
		tj, _ := time.Parse("2006-01-02", dj[1])
		return ti.Before(tj)
	})
}

// copyFile copies file content from a file given its path into a writer.
func copyFile(dst io.Writer, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(dst, src)
	return err
}

// writeConcat writes the concatenation of filenames (in order) to tmpPath.
// The caller is responsible for renaming tmpPath into place and for removing
// it on error.
func writeConcat(dir string, filenames []string, tmpPath string) error {
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	for _, filename := range filenames {
		full := filepath.Join(dir, filename)
		if err := copyFile(out, full); err != nil {
			out.Close()
			return fmt.Errorf("copy %s: %w", full, err)
		}
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	return nil
}

func colorizeFileCount(count int) string {
	var color string
	switch {
	case count < 500:
		color = "\033[33m" // yellow - small batches
	case count < 1000:
		color = "\033[36m" // cyan - medium batches
	default:
		color = "\033[35m" // magenta - large batches
	}
	return fmt.Sprintf("%s%d\033[0m", color, count)
}
