package store

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/miku/metha/oai"
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

	// MaxRecordBytes and Oversize bound and report what one record may cost.
	// See ReadOptions, which is where they take effect.
	MaxRecordBytes int
	Oversize       func(identifier string, n int)

	// Endpoint, when set, is written into every JSON line as an "endpoint"
	// field.
	//
	// A record does not say where it came from: OAI-PMH gives it a header, a
	// metadata element and an about, and nothing that names the repository. For
	// one endpoint's output that is fine, because the question was asked about
	// an endpoint. For a corpus dump it is not - a few hundred million lines
	// drawn from a quarter of a million repositories, with no way back to which
	// one said what.
	//
	// XML output ignores this. An attribute on the record element would change
	// the shape of the record itself, and a corpus is exported as JSON.
	Endpoint string
}

// jsonRecord is a record with its provenance. The embedding is what keeps the
// line the same shape it has always been: the record's own fields stay at the
// top level, in their order, and "endpoint" is appended rather than wrapped
// around them. A consumer reading .header.identifier does not care that the
// field arrived.
type jsonRecord struct {
	oai.Record
	Endpoint string `json:"endpoint,omitempty"`
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
		From:           opts.From,
		Until:          opts.Until,
		SetSpec:        opts.SetSpec,
		Deleted:        opts.Deleted,
		MaxRecordBytes: opts.MaxRecordBytes,
		Oversize:       opts.Oversize,
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
func renderRecord(rec oai.Record, opts RenderOpts) error {
	var (
		b   []byte
		err error
	)
	switch {
	case opts.UseJson && opts.Endpoint != "":
		b, err = json.Marshal(jsonRecord{Record: rec, Endpoint: opts.Endpoint})
	case opts.UseJson:
		b, err = json.Marshal(rec)
	default:
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
