package cli

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/miku/metha"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// $ time metha export | zstd -c -T0 > metha-export-2026-09-03.json.zst
// 162,377,654 records from 97,227 endpoints in 19m28s, 370.2GB; 11,800 endpoints matched nothing
//
// real    22m42.321s
// user    488m0.518s
// sys     7m47.819s

// newExportCmd writes the whole cache out as one stream of records.
//
// It is metha cat over every endpoint at once, and it exists because the corpus
// is the artefact. What it replaces is a shell pipeline - find the files,
// decompress them in parallel, feed them to a separate XML scanner - which
// works, and which cannot say which repository a record came from, cannot apply
// a datestamp bound, and cannot tell a shard it failed to read from one that
// held nothing.
//
// The one thing it adds to a record is where the record was found. See
// store.RenderOpts.Endpoint for why that is not optional in a corpus dump.
func newExportCmd() *cobra.Command {
	var o exportOpts
	cmd := &cobra.Command{
		Use:   "export [ENDPOINT...]",
		Short: "Write every harvested record to one stream",
		Long: `export writes the records of every harvested endpoint to stdout, or to a
file, as one JSON document per line. Named endpoints, or the ones in --endpoints,
narrow it; with neither it is the whole cache.

Each line carries an "endpoint" field naming the repository the record came
from, which is the one thing an OAI-PMH record does not say about itself and
the one thing a corpus of them needs.

A record whose metadata runs past --max-record-bytes is left out, and the count
and the endpoints it came from are reported at the end. Nothing a repository
means to publish comes near the default, so the bound is a valve rather than a
filter: it is what turns one impossible record into a line of stderr naming the
repository it came from, instead of a run that renders --jobs of them at once
and is killed. Pass 0 for no bound.

It reads and never writes the cache, and takes no locks, so it is safe to run
while a sweep is harvesting. What it sees is the cache as of the moment it
reads each shard: windows commit whole, so a shard being written to yields the
records committed so far and never a half-written one.`,
		Example: `  metha export > corpus.ndjson
  metha export -o corpus.ndjson.zst              # compressed by extension
  metha export --from 2024-01-01                 # only recent records
  metha export http://export.arxiv.org/oai2      # one endpoint
  metha endpoints --state active > live.txt
  metha export --endpoints live.txt -o live.ndjson.zst`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd.Context(), args)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.StringVar(&o.format, "format", "oai_dc", "metadata format to export")
	f.StringVar(&o.set, "set", "", "set name to export")
	f.StringVarP(&o.output, "output", "o", "", "write here rather than to stdout; .zst or .gz compresses")
	f.StringVar(&o.listFile, "endpoints", "", "export the endpoints named in this file, one per line")
	f.StringVar(&o.from, "from", "", "ignore records before this date")
	f.StringVar(&o.until, "until", "", "ignore records after this date")
	f.StringVar(&o.setSpec, "setspec", "", "only records carrying this setSpec")
	f.BoolVar(&o.deleted, "deleted", false, "include records the endpoint marked deleted")
	f.BoolVar(&o.onlyDeleted, "only-deleted", false, "emit only the records the endpoint marked deleted")
	f.BoolVar(&o.asXML, "xml", false, "one XML document rather than JSON lines")
	f.StringVar(&o.root, "root", "Records", "root element to wrap the XML in")
	f.BoolVar(&o.noEndpoint, "no-endpoint", false, "leave the endpoint field out of each JSON line")
	f.IntVar(&o.jobs, "jobs", runtime.NumCPU(), "endpoints to read in parallel")
	f.IntVar(&o.maxRecordBytes, "max-record-bytes", defaultMaxRecordBytes,
		"drop records whose metadata exceeds this many bytes; 0 for no bound")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "no progress counter")
	return cmd
}

// exportOpts is one invocation of export.
type exportOpts struct {
	baseDir     string
	format      string
	set         string
	output      string
	listFile    string
	from        string
	until       string
	setSpec     string
	deleted     bool
	onlyDeleted bool
	asXML       bool
	root        string
	noEndpoint  bool
	jobs        int
	quiet       bool

	maxRecordBytes int
}

// defaultMaxRecordBytes is how large one record's metadata may be before export
// leaves it out.
//
// A record is a few kilobytes; a large one - marcxml, mets - is a few hundred.
// Measured over a cache of 394 endpoints, 338,368 records read with no bound at
// all ran to 2.4KB at the median, 9.2KB at the 99th centile and 67KB at the
// largest. Sixteen megabytes is two hundred times that largest, which is the
// point: this is not meant to fire, and a corpus where it starts firing is one
// with something wrong in it worth being told about.
//
// It is deliberately far above anything real because dropping records is the
// expensive mistake. What it buys is that the memory of an export is predictable
// rather than a property of the worst record in the corpus: rendering costs about
// 7x the record and --jobs of them run at once, so this number, times the job
// count, says what the run will need. See store.ReadOptions.MaxRecordBytes.
const defaultMaxRecordBytes = 16 << 20

// chunkSize is how much a worker accumulates before handing it to the writer.
//
// Records go through the chunker one whole line per Write, so any threshold
// keeps the output line-aligned; what the size buys is that the writer is woken
// once per quarter megabyte rather than once per record. It also bounds memory:
// a repository with two million records would otherwise be rendered whole
// before any of it could be written.
const chunkSize = 256 << 10

// maxNamed bounds how many not-yet-harvested endpoints are named before the
// count speaks for them. Exporting a list of five thousand URLs from a roster
// where most have never answered should not print five thousand lines to say
// so, and the first few are as informative as all of them.
const maxNamed = 10

func (o *exportOpts) run(ctx context.Context, args []string) error {
	if o.jobs < 1 {
		o.jobs = 1
	}
	targets, err := o.targets(args)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to export")
		warnLegacy(o.baseDir)
		return nil
	}

	// A write that cannot land - a full disk, or a reader that went away
	// because someone piped this into head - has to stop the whole export
	// rather than be discovered again once per endpoint.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out, closeOut, err := o.open()
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(out, 1<<20)

	// The counter goes to stderr and the records to stdout, so the two never
	// meet. progress is handed io.Discard for its data stream because this
	// command's stdout belongs to the records: nothing else may write a byte
	// there.
	p := newProgress(os.Stderr, io.Discard, StderrIsTerminal(), o.quiet, "exporting", len(targets))
	defer p.stop()

	if o.asXML && o.root != "" {
		// Written once around the whole export rather than once per endpoint,
		// which is the difference between one document and a quarter of a
		// million concatenated ones.
		if _, err := fmt.Fprintf(bw, "<%s xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">\n", o.root); err != nil {
			return errors.Join(err, closeOut())
		}
	}

	started := time.Now()
	chunks := make(chan []byte, o.jobs*2)
	work := make(chan store.Identity)
	results := make(chan exportResult)

	var wg sync.WaitGroup
	for range o.jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range work {
				p.begin(id.BaseURL)
				results <- o.one(ctx, id, chunks)
			}
		}()
	}
	go func() {
		defer close(work)
		for _, id := range targets {
			select {
			case work <- id:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
		close(chunks)
	}()

	// One writer, so the order of lines is the order the chunks arrive and no
	// two workers can interleave inside a line.
	var (
		written  int64
		writeErr error
		done     = make(chan struct{})
	)
	go func() {
		defer close(done)
		for c := range chunks {
			if writeErr != nil {
				// Drained rather than abandoned: the workers are still sending,
				// and a channel nobody reads is a pool that never finishes.
				continue
			}
			n, err := bw.Write(c)
			written += int64(n)
			if err != nil {
				writeErr = err
				cancel()
			}
		}
	}()

	var (
		records  int64
		failed   int
		read     int
		empty    int
		missing  int
		oversize int
		bloated  int // endpoints holding at least one such record
		largest  int
	)
	for r := range results {
		// Reported as it happens, not only in the summary: a dropped record is
		// the one outcome here that names a repository worth looking at, and by
		// the time the summary prints, the run that would have led someone to it
		// is over.
		if r.oversize > 0 {
			oversize += r.oversize
			bloated++
			largest = max(largest, r.largest)
			if bloated <= maxNamed {
				p.printf("%s: %s over --max-record-bytes, largest %s",
					r.id.BaseURL, plural2(r.oversize, "record"), humanBytes(int64(r.largest)))
			}
		}
		switch {
		// The cache holding nothing for an endpoint is a fact about the cache,
		// not a failed read, and the store is careful to say which it means.
		// Over a list this matters: metha endpoints prints URLs the sweep knows
		// about, and plenty of those have never been harvested successfully.
		// Failing the whole export over them would make the composition the
		// README recommends useless.
		case errors.Is(r.err, store.ErrNotHarvested):
			missing++
			if missing <= maxNamed {
				p.printf("%s: nothing harvested yet", r.id.BaseURL)
			}
		case r.err != nil:
			failed++
			p.printf("%s: %v", r.id.BaseURL, r.err)
		default:
			read++
			// Read, and held nothing that matched. Worth counting apart from
			// the above because the action is different: this one is a --from
			// past the end of the cache, or a setspec nothing carries, and a
			// run that exported three records from a list of five thousand
			// endpoints should say so rather than leave it to be noticed.
			if r.records == 0 {
				empty++
			}
		}
		records += int64(r.records)
		p.step(r.bytes, r.err != nil)
	}
	<-done
	p.stop()

	if writeErr == nil && o.asXML && o.root != "" {
		_, writeErr = fmt.Fprintf(bw, "</%s>\n", o.root)
	}
	// Flushed before the file is closed, and both errors kept. A corpus dump
	// that failed on the last buffer and exited 0 is the one outcome nobody
	// would notice until they came to read it.
	err = errors.Join(writeErr, bw.Flush(), closeOut())

	fmt.Fprintf(os.Stderr, "%s from %s in %s, %s",
		plural2(int(records), "record"), plural2(read, "endpoint"),
		duration(time.Since(started)), humanBytes(written))
	if empty > 0 {
		fmt.Fprintf(os.Stderr, "; %s matched nothing", plural2(empty, "endpoint"))
	}
	if missing > 0 {
		fmt.Fprintf(os.Stderr, "; %s never harvested", thousands(missing))
	}
	if oversize > 0 {
		fmt.Fprintf(os.Stderr, "; %s over --max-record-bytes at %s dropped from %s, largest %s",
			plural2(oversize, "record"), humanBytes(int64(o.maxRecordBytes)),
			plural2(bloated, "endpoint"), humanBytes(int64(largest)))
	}
	fmt.Fprintln(os.Stderr)
	warnLegacy(o.baseDir)

	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted after %s of %s",
			thousands(read+failed), plural2(len(targets), "endpoint"))
	}
	if failed > 0 {
		// Non-zero on purpose. Every other outcome here is a complete dump, and
		// a partial one that says so only in a line of stderr is a partial one
		// that gets published.
		return fmt.Errorf("%s could not be read; the export is incomplete",
			plural2(failed, "endpoint"))
	}
	return nil
}

// exportResult is what one endpoint contributed.
type exportResult struct {
	id      store.Identity
	records int
	bytes   int64
	err     error

	// oversize is how many records the size bound left out, and largest is the
	// biggest of them. Kept per endpoint because that is the unit anyone acts
	// on: a corpus dump that quietly lost records should be able to say which
	// repository to go and look at.
	oversize int
	largest  int
}

// one renders a single endpoint into the chunk stream.
func (o *exportOpts) one(ctx context.Context, id store.Identity, chunks chan<- []byte) exportResult {
	st, err := store.Open(o.baseDir, id)
	if err != nil {
		return exportResult{id: id, err: err}
	}
	c := &chunker{ctx: ctx, out: chunks}
	res := exportResult{id: id}
	err = store.Render(st, store.RenderOpts{
		Writer: c,
		// Empty: the root element belongs to the export, not to each endpoint.
		Root:           "",
		From:           o.from,
		Until:          o.until,
		SetSpec:        o.setSpec,
		Deleted:        o.deletedPolicy(),
		UseJson:        !o.asXML,
		Endpoint:       o.endpointField(id),
		MaxRecordBytes: o.maxRecordBytes,
		// Called from this endpoint's own worker, so the counters below need no
		// lock: one goroutine renders one endpoint.
		Oversize: func(_ string, n int) {
			res.oversize++
			res.largest = max(res.largest, n)
		},
	})
	if ferr := c.flush(); err == nil {
		err = ferr
	}
	res.records, res.bytes, res.err = c.records, c.written, err
	return res
}

// endpointField is the provenance written into each line, or the empty string
// when there is none to write.
func (o *exportOpts) endpointField(id store.Identity) string {
	if o.asXML || o.noEndpoint {
		return ""
	}
	return id.BaseURL
}

func (o *exportOpts) deletedPolicy() store.DeletedPolicy {
	switch {
	case o.onlyDeleted:
		return store.DeletedOnly
	case o.deleted:
		return store.DeletedKeep
	default:
		return store.DeletedSkip
	}
}

// targets is what to export: the endpoints named on the command line and in
// --endpoints, or every group in the cache matching the format and set.
func (o *exportOpts) targets(args []string) ([]store.Identity, error) {
	urls := args
	if o.listFile != "" {
		named, err := readEndpointList(o.listFile)
		if err != nil {
			return nil, err
		}
		urls = append(urls, named...)
	}
	if len(urls) > 0 {
		ids := make([]store.Identity, 0, len(urls))
		for _, u := range urls {
			ids = append(ids, store.Identity{
				BaseURL: oai.PrependSchema(u),
				Format:  o.format,
				Set:     o.set,
			})
		}
		return ids, nil
	}
	var ids []store.Identity
	for e, err := range store.List(o.baseDir) {
		if err != nil {
			log.Printf("skipping: %v", err)
			continue
		}
		// Matched exactly, the way an identity is matched everywhere else: an
		// endpoint harvested in two formats is two groups, and exporting both
		// into one stream would produce lines that no longer agree about what
		// their metadata element holds.
		if e.Identity.Format != o.format || e.Identity.Set != o.set {
			continue
		}
		ids = append(ids, e.Identity)
	}
	return ids, nil
}

// readEndpointList reads endpoint URLs from a file, one per line. Blank lines
// and comments are skipped, so the output of metha endpoints is an input here
// whether or not somebody annotated it on the way past.
func readEndpointList(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var urls []string
	for line := range strings.Lines(string(b)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no endpoints in %s", path)
	}
	return urls, nil
}

// open returns the stream the records go to and the function that closes it.
//
// Compression follows the file name, because the extension is what the file
// will be read by and a dump named .zst that is not zstd is worse than an
// uncompressed one. stdout is left alone: a pipeline can compress it, and
// guessing there would mean guessing from nothing.
func (o *exportOpts) open() (io.Writer, func() error, error) {
	if o.output == "" {
		return os.Stdout, func() error { return nil }, nil
	}
	f, err := os.Create(o.output)
	if err != nil {
		return nil, nil, err
	}
	switch strings.ToLower(filepath.Ext(o.output)) {
	case ".zst":
		enc, err := zstd.NewWriter(f)
		if err != nil {
			return nil, nil, errors.Join(err, f.Close())
		}
		return enc, func() error { return errors.Join(enc.Close(), f.Close()) }, nil
	case ".gz":
		gz := gzip.NewWriter(f)
		return gz, func() error { return errors.Join(gz.Close(), f.Close()) }, nil
	default:
		return f, f.Close, nil
	}
}

// warnLegacy says so when part of the cache was not exported because it is
// still in the pre-1.0 layout. Silence there would read as an endpoint that
// holds nothing, which is the one answer that is both wrong and plausible.
func warnLegacy(baseDir string) {
	if n := store.LegacyRemainder(baseDir); n > 0 {
		fmt.Fprintf(os.Stderr,
			"%s still in the pre-1.0 layout and not exported; run metha migrate\n",
			plural2(n, "endpoint"))
	}
}

// chunker collects rendered records and hands them to the writer in blocks.
//
// Render calls Write exactly once per record, with the whole line, so anything
// this buffer ships is a whole number of lines however it is cut. That is what
// lets several endpoints be rendered at once into one stream: the workers can
// interleave between lines and never inside one.
type chunker struct {
	ctx     context.Context
	out     chan<- []byte
	buf     bytes.Buffer
	records int
	written int64
}

func (c *chunker) Write(p []byte) (int, error) {
	c.records++
	c.written += int64(len(p))
	c.buf.Write(p)
	if c.buf.Len() >= chunkSize {
		return len(p), c.flush()
	}
	return len(p), nil
}

// flush hands off what has been collected. The copy is not avoidable: the
// buffer is reused for the next block and the chunk outlives this call.
func (c *chunker) flush() error {
	if c.buf.Len() == 0 {
		return nil
	}
	b := bytes.Clone(c.buf.Bytes())
	c.buf.Reset()
	select {
	case c.out <- b:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}
