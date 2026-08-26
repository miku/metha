package store

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/miku/metha"
)

// RenderOpts controls output by the metha-cat command.
type RenderOpts struct {
	Writer  io.Writer
	Root    string
	From    string
	Until   string
	SetSpec string
	Deleted DeletedPolicy
	UseJson bool
}

// Render writes every record of s matching the datestamp bounds to the writer,
// one per line, as XML or JSON. A non-empty Root wraps the XML output in that
// element, so the result is a single well formed document.
func Render(s Store, opts RenderOpts) error {
	if opts.Root != "" && !opts.UseJson {
		if _, err := fmt.Fprintf(opts.Writer,
			"<%s xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n", opts.Root); err != nil {
			return err
		}
	}
	read := ReadOptions{
		From:    opts.From,
		Until:   opts.Until,
		SetSpec: opts.SetSpec,
		Deleted: opts.Deleted,
	}
	for rec, err := range s.Records(read) {
		if err != nil {
			return err
		}
		if err := renderRecord(rec, opts); err != nil {
			return err
		}
	}
	if opts.Root != "" && !opts.UseJson {
		if _, err := fmt.Fprintf(opts.Writer, "</%s>\n", opts.Root); err != nil {
			return err
		}
	}
	return nil
}

// renderRecord marshals a single record and writes it as one line.
func renderRecord(rec metha.Record, opts RenderOpts) error {
	var (
		b   []byte
		err error
	)
	if opts.UseJson {
		b, err = json.Marshal(rec)
	} else {
		rec.XMLName = xml.Name{Local: "record", Space: "http://www.openarchives.org/OAI/2.0/"}
		b, err = xml.Marshal(rec)
	}
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}
	if _, err := opts.Writer.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}
	return nil
}
