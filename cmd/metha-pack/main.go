package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/miku/metha"
)

var (
	baseDir  = flag.String("d", metha.GetBaseDir(), "base directory for harvested files")
	minFiles = flag.Int("m", 3, "minimum number of files before packing")
	verbose  = flag.Bool("v", false, "verbose output")
	dryRun   = flag.Bool("r", false, "show what would be done without actually doing it")
)

type Stats struct {
	TotalDirs     int
	ProcessedDirs int
	SkippedDirs   int
	TotalFiles    int
	PackedFiles   int
	BytesSaved    int64
}

func main() {
	log.SetOutput(os.Stderr)
	flag.Parse()

	var root string
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	} else {
		root = *baseDir
	}

	fmt.Fprintf(os.Stderr, "Analyzing directory structure: %s\n", root)

	stats := &Stats{}
	if *dryRun {
		fmt.Fprintf(os.Stderr, "DRY RUN MODE - no files will be modified\n")
	}

	// Process directories in streaming fashion
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() || path == root {
			return nil
		}

		stats.TotalDirs++
		if stats.TotalDirs%100 == 0 {
			fmt.Fprintf(os.Stderr, "Processed %d directories...\n", stats.TotalDirs)
		}

		processDirectory(path, stats)
		return nil
	})

	if err != nil {
		log.Fatalf("Error walking directories: %v", err)
	}

	fmt.Fprintf(os.Stderr, "\n=== Final Statistics ===\n")
	fmt.Fprintf(os.Stderr, "Directories processed: %d/%d\n", stats.ProcessedDirs, stats.TotalDirs)
	fmt.Fprintf(os.Stderr, "Directories skipped: %d\n", stats.SkippedDirs)
	fmt.Fprintf(os.Stderr, "Files packed: %d\n", stats.PackedFiles)
	fmt.Fprintf(os.Stderr, "Total files before: %d\n", stats.TotalFiles)
	if stats.BytesSaved > 0 {
		fmt.Fprintf(os.Stderr, "Estimated metadata overhead saved: %.2f MB\n", float64(stats.BytesSaved)/(1024*1024))
	}
}

func isCompressedFile(filename string) bool {
	return strings.HasSuffix(filename, ".zst") || strings.HasSuffix(filename, ".gz")
}

func processDirectory(path string, stats *Stats) {
	files, err := os.ReadDir(path)
	if err != nil {
		if *verbose {
			log.Printf("Warning: cannot read directory %s: %v", path, err)
		}
		stats.SkippedDirs++
		return
	}

	// Count and collect compressed files in one pass
	var compressedFiles []string
	for _, file := range files {
		if isCompressedFile(file.Name()) {
			compressedFiles = append(compressedFiles, file.Name())
		}
	}

	if len(compressedFiles) < *minFiles {
		if *verbose {
			log.Printf("Skipped %s: only %d files (minimum: %d)", filepath.Base(path), len(compressedFiles), *minFiles)
		}
		stats.SkippedDirs++
		return
	}

	// Sort by date (no need to worry about extension since same date = same extension)
	sortFilesByDate(compressedFiles)
	latestFile := compressedFiles[len(compressedFiles)-1]

	log.Printf("Processing %s: packing %d files into %s", filepath.Base(path), len(compressedFiles), latestFile)

	stats.TotalFiles += len(compressedFiles)
	stats.PackedFiles += len(compressedFiles)

	if *dryRun {
		stats.ProcessedDirs++
		stats.BytesSaved += int64(len(compressedFiles)-1) * 4096
		return
	}

	// Concatenate files - explicit cleanup, no defer in loop
	targetPath := filepath.Join(path, latestFile)
	tmpPath := filepath.Join(path, ".tmp_concat")

	if !concatenateFiles(path, compressedFiles, tmpPath, targetPath) {
		stats.SkippedDirs++
		return
	}

	// Delete other files
	deletedCount := 0
	for _, filename := range compressedFiles {
		if filename != latestFile {
			fullPath := filepath.Join(path, filename)
			if err := os.Remove(fullPath); err != nil {
				if *verbose {
					log.Printf("Warning: failed to delete %s: %v", fullPath, err)
				}
			} else {
				deletedCount++
			}
		}
	}

	stats.ProcessedDirs++
	stats.BytesSaved += int64(deletedCount) * 4096
	log.Printf("✓ Packed %d files, deleted %d files in %s", len(compressedFiles), deletedCount, filepath.Base(path))
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

func concatenateFiles(dir string, filenames []string, tmpPath, targetPath string) bool {
	out, err := os.Create(tmpPath)
	if err != nil {
		log.Printf("Error creating temp file: %v", err)
		return false
	}

	success := true
	for _, filename := range filenames {
		fullPath := filepath.Join(dir, filename)
		in, err := os.Open(fullPath)
		if err != nil {
			log.Printf("Error opening %s: %v", fullPath, err)
			success = false
			in.Close() // safe to call on nil
			break
		}

		_, err = io.Copy(out, in)
		in.Close() // explicit close, no defer

		if err != nil {
			log.Printf("Error copying %s: %v", fullPath, err)
			success = false
			break
		}
	}

	out.Close() // explicit close

	if !success {
		os.Remove(tmpPath) // cleanup on failure
		return false
	}

	// Atomic replace
	if err := os.Rename(tmpPath, targetPath); err != nil {
		log.Printf("Error replacing file: %v", err)
		os.Remove(tmpPath) // cleanup on failure
		return false
	}

	return true
}
