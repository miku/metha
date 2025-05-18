package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"compress/gzip"

	"github.com/karrick/godirwalk"
	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha"
)

var (
	cacheDir         = flag.String("d", metha.GetBaseDir(), "metha cache directory to convert")
	compressionLevel = flag.Int("l", 3, "zstd compression level (-5 to 22)")
	dryRun           = flag.Bool("D", false, "only show what would be done without making changes")
	removeOriginal   = flag.Bool("f", false, "remove original gzip files after conversion")
	numWorkers       = flag.Int("w", 4, "number of parallel workers")
)

func main() {
	flag.Parse()

	var gzipFiles []string

	err := godirwalk.Walk(*cacheDir, &godirwalk.Options{
		Callback: func(path string, de *godirwalk.Dirent) error {
			if !de.IsDir() && strings.HasSuffix(path, ".xml.gz") {
				gzipFiles = append(gzipFiles, path)
				if numFound := len(gzipFiles); numFound%10_000_000 == 0 && numFound > 0 {
					fmt.Fprintf(os.Stderr, "found % 10d files [...]\n", numFound)
				}
			}
			return nil
		},
		Unsorted: true, // For faster traversal
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(os.Stderr, "found %d gzip files to convert\n", len(gzipFiles))
	if *dryRun {
		for _, file := range gzipFiles {
			fmt.Println(file)
		}
		return
	}

	jobs := make(chan string, len(gzipFiles))
	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < *numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				destFile := strings.TrimSuffix(file, ".gz") + ".zst"

				if _, err := os.Stat(destFile); err == nil {
					continue
				}

				if err := convertFile(file, destFile, *compressionLevel); err != nil {
					log.Fatal(err)
				}

				if *removeOriginal {
					if err := os.Remove(file); err != nil {
						log.Fatal(err)
					}
				}
			}
		}()
	}

	for _, file := range gzipFiles {
		jobs <- file
	}
	close(jobs)

	wg.Wait()
	fmt.Println("Conversion complete!")
}

func convertFile(src, dst string, level int) error {
	// First check if the source file is empty
	fileInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Special handling for 0-byte files
	if fileInfo.Size() == 0 {
		// For 0-byte files, just create an empty zstd file
		tmpDst := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
		dstFile, err := os.Create(tmpDst)
		if err != nil {
			return fmt.Errorf("failed to create destination file: %w", err)
		}

		// Create and close the zstd writer to write proper zstd headers
		zstdOpts := zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level))
		zstdWriter, err := zstd.NewWriter(dstFile, zstdOpts)
		if err != nil {
			dstFile.Close()
			os.Remove(tmpDst)
			return fmt.Errorf("failed to create zstd writer: %w", err)
		}

		// Close the zstd writer to write the frame
		if err := zstdWriter.Close(); err != nil {
			dstFile.Close()
			os.Remove(tmpDst)
			return fmt.Errorf("failed to close zstd writer: %w", err)
		}

		// Close the file and rename
		if err := dstFile.Close(); err != nil {
			os.Remove(tmpDst)
			return fmt.Errorf("failed to close destination file: %w", err)
		}

		if err := os.Rename(tmpDst, dst); err != nil {
			return fmt.Errorf("failed to rename temp file: %w", err)
		}

		return nil
	}

	// Original code for non-empty files
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	gzReader, err := gzip.NewReader(srcFile)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	tmpDst := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
	dstFile, err := os.Create(tmpDst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		dstFile.Close()
		if err != nil {
			os.Remove(tmpDst)
		}
	}()

	zstdOpts := zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level))
	zstdWriter, err := zstd.NewWriter(dstFile, zstdOpts)
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %w", err)
	}
	defer zstdWriter.Close()

	if _, err := io.Copy(zstdWriter, gzReader); err != nil {
		return fmt.Errorf("failed to copy data (%s): %w", src, err)
	}

	if err := zstdWriter.Close(); err != nil {
		return fmt.Errorf("failed to close zstd writer: %w", err)
	}

	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("failed to close destination file: %w", err)
	}

	if err := os.Rename(tmpDst, dst); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}
