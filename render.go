package metha

import (
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
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

// RenderHarvest renders harvest to JSON or XML.
func Render(opts *RenderOpts) error {
	files, err := ioutil.ReadDir(opts.Harvest.Dir())
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
		if !strings.HasSuffix(file.Name(), ".xml.gz") {
			continue
		}
		if opts.From != "" && file.Name() < opts.From {
			continue
		}
		abspath := filepath.Join(opts.Harvest.Dir(), file.Name())
		fi, err := os.Open(abspath)
		if err != nil {
			return err
		}
		r, err := gzip.NewReader(fi)
		if err != nil {
			return err
		}
		dec := xml.NewDecoder(r)
		dec.Strict = false
		var (
			resp Response
			b    []byte
		)
		if err := dec.Decode(&resp); err != nil {
			return err
		}
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
				return err
			}
			if _, err := io.WriteString(opts.Writer, string(b)+"\n"); err != nil {
				return err
			}
		}
	}
	return nil
}
