package metha

import (
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// RenderOpts controls output by the metha-cat command.
type RenderOpts struct {
	Writer  io.Writer
	Harvest Harvest
	Root    string
	From    string
	Until   string
	UseJson bool
}

func Render(opts *RenderOpts) error {
	files, err := os.ReadDir(opts.Harvest.Dir())
	if err != nil {
		return err
	}

	if opts.Root != "" && !opts.UseJson {
		if _, err := fmt.Fprintf(opts.Writer,
			"<%s xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n", opts.Root); err != nil {
			return err
		}
		defer func() {
			fmt.Fprintf(opts.Writer, "</%s>\n", opts.Root)
		}()
	}

	for _, file := range files {
		if err := processFile(file, opts); err != nil {
			return err
		}
	}
	return nil
}

func processFile(file os.DirEntry, opts *RenderOpts) error {
	fileName := file.Name()
	if !strings.HasSuffix(fileName, ".xml.gz") && !strings.HasSuffix(fileName, ".xml.zst") {
		return nil
	}
	if opts.From != "" && fileName < opts.From {
		return nil
	}

	abspath := filepath.Join(opts.Harvest.Dir(), fileName)
	fi, err := os.Open(abspath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", abspath, err)
	}
	defer fi.Close()

	// Create appropriate reader based on file extension
	var xmlReader io.Reader
	if strings.HasSuffix(fileName, ".xml.gz") {
		r, err := gzip.NewReader(fi)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader for %s: %w", abspath, err)
		}
		defer r.Close()
		xmlReader = r
	} else if strings.HasSuffix(fileName, ".xml.zst") {
		r, err := zstd.NewReader(fi)
		if err != nil {
			return fmt.Errorf("failed to create zstd reader for %s: %w", abspath, err)
		}
		defer r.Close()
		xmlReader = r
	} else {
		// This shouldn't happen based on earlier check, but just in case
		return fmt.Errorf("unsupported file format: %s", fileName)
	}

	// Decode the XML. A single file can hold more than one response:
	// metha-pack concatenates compressed frames into one file, and both the
	// gzip and the zstd reader stream those members transparently.
	dec := xml.NewDecoder(xmlReader)
	dec.Strict = false
	for {
		var resp Response
		if err := dec.Decode(&resp); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("failed to decode XML from %s: %w", abspath, err)
		}
		if err := renderRecords(&resp, opts); err != nil {
			return err
		}
	}
}

// renderRecords writes the records of a single response, applying the
// datestamp filters.
func renderRecords(resp *Response, opts *RenderOpts) error {
	var (
		b   []byte
		err error
	)
	for _, rec := range resp.ListRecords.Records {
		if opts.From != "" && rec.Header.DateStamp < opts.From {
			continue
		}
		if opts.Until != "" && rec.Header.DateStamp > opts.Until {
			continue
		}

		if opts.UseJson {
			b, err = json.Marshal(rec)
		} else {
			rec.XMLName = xml.Name{Local: "record", Space: "http://www.openarchives.org/OAI/2.0/"}
			b, err = xml.Marshal(rec)
		}
		if err != nil {
			return fmt.Errorf("failed to marshal record: %w", err)
		}

		if _, err := io.WriteString(opts.Writer, string(b)+"\n"); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
	}
	return nil
}
