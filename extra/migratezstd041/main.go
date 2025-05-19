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
	keepOriginal     = flag.Bool("F", false, "keep gzip after conversion (not recommeded)")
	numWorkers       = flag.Int("w", 4, "number of parallel workers")
	bestEffort       = flag.Bool("B", false, "best effort, only log errors, do not halt")
	forceRemove      = flag.Bool("f", false, "remove existing gzip file, if zstd file is already present (weaker than -F)")
)

func main() {
	flag.Parse()
	var gzipFiles []string
	err := godirwalk.Walk(*cacheDir, &godirwalk.Options{
		Callback: func(path string, de *godirwalk.Dirent) error {
			if !de.IsDir() && strings.HasSuffix(path, ".xml.gz") {
				gzipFiles = append(gzipFiles, path)
				if numFound := len(gzipFiles); numFound%10_000_000 == 0 && numFound > 0 {
					log.Printf("walk: found %d files [...]", numFound)
				}
			}
			return nil
		},
		Unsorted: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(gzipFiles) == 0 {
		log.Println("nothing to do")
		os.Exit(0)
	}
	log.Printf("found %d gzip file(s) to convert", len(gzipFiles))
	if *dryRun {
		for _, file := range gzipFiles {
			fmt.Println(file)
		}
		return
	}
	jobs := make(chan string, len(gzipFiles))
	var wg sync.WaitGroup
	for w := 0; w < *numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				destFile := strings.TrimSuffix(file, ".gz") + ".zst"
				if _, err := os.Stat(destFile); err == nil {
					if !*keepOriginal && *forceRemove {
						if err := os.Remove(file); err != nil {
							log.Fatal(err)
						}
					} else {
						continue
					}
				}
				if err := convertFile(file, destFile, *compressionLevel); err != nil {
					log.Fatal(err)
				}
				if !*keepOriginal {
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
}

func convertFile(src, dst string, level int) error {
	fileInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}
	// handle like invalid gzip files
	if fileInfo.Size() < 20 {
		tmpDst := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
		dstFile, err := os.Create(tmpDst)
		if err != nil {
			return fmt.Errorf("failed to create destination file: %w", err)
		}
		zstdOpts := zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level))
		zstdWriter, err := zstd.NewWriter(dstFile, zstdOpts)
		if err != nil {
			dstFile.Close()
			os.Remove(tmpDst)
			return fmt.Errorf("failed to create zstd writer: %w", err)
		}
		if err := zstdWriter.Close(); err != nil {
			dstFile.Close()
			os.Remove(tmpDst)
			return fmt.Errorf("failed to close zstd writer: %w", err)
		}
		if err := dstFile.Close(); err != nil {
			os.Remove(tmpDst)
			return fmt.Errorf("failed to close destination file: %w", err)
		}
		if err := os.Rename(tmpDst, dst); err != nil {
			return fmt.Errorf("failed to rename temp file: %w", err)
		}
		return nil
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	gzReader, err := gzip.NewReader(srcFile)
	if err != nil {
		// If we still can't create a gzip reader despite the file size check,
		// the file might not be a valid gzip file. Create an empty zstd file instead.
		srcFile.Close()

		tmpDst := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
		dstFile, err := os.Create(tmpDst)
		if err != nil {
			return fmt.Errorf("failed to create destination file: %w", err)
		}
		zstdOpts := zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level))
		zstdWriter, err := zstd.NewWriter(dstFile, zstdOpts)
		if err != nil {
			dstFile.Close()
			os.Remove(tmpDst)
			return fmt.Errorf("failed to create zstd writer: %w", err)
		}
		if err := zstdWriter.Close(); err != nil {
			dstFile.Close()
			os.Remove(tmpDst)
			return fmt.Errorf("failed to close zstd writer: %w", err)
		}
		if err := dstFile.Close(); err != nil {
			os.Remove(tmpDst)
			return fmt.Errorf("failed to close destination file: %w", err)
		}
		if err := os.Rename(tmpDst, dst); err != nil {
			return fmt.Errorf("failed to rename temp file: %w", err)
		}
		return nil
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
		if *bestEffort {
			log.Printf("failed to copy likely broken file: %v", src)
			return nil
		}
		return fmt.Errorf("failed to copy data (%v): %w", src, err)
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
