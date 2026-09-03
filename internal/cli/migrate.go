package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/miku/metha"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// newMigrateCmd converts harvested data from the pre-1.0 layout, a directory of
// compressed responses per endpoint, into shards. It is the only command that
// reads that layout at all; everything else refuses it and points here.
//
// No re-harvest is needed: those files hold complete responses, so a shard can
// be built from the cache alone. The source is left in place unless --rm is
// given, and then only when the whole of it was converted; see migrateOne.
func newMigrateCmd() *cobra.Command {
	var o migrateOpts
	cmd := &cobra.Command{
		Use:   "migrate [ENDPOINT...]",
		Short: "Convert pre-1.0 directories into shards",
		Example: `  metha migrate                          # every endpoint in the cache
  metha migrate http://export.arxiv.org/oai2
  metha migrate --dry-run --verbose      # what would happen, with sizes
  metha migrate --rm                     # convert, then drop the sources
  metha migrate --jobs 32                # a big cache, in parallel`,
		Aliases: []string{"metha-migrate"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd.Context(), args)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.StringVar(&o.format, "format", "oai_dc", "metadata format, when an endpoint is named")
	f.StringVar(&o.set, "set", "", "set name, when an endpoint is named")
	f.BoolVar(&o.remove, "rm", false, "remove the old directory after a verified migration")
	f.BoolVar(&o.dryRun, "dry-run", false, "only report what would be migrated")
	f.BoolVar(&o.verbose, "verbose", false, "report every endpoint, not just the ones with something to say")
	f.IntVar(&o.jobs, "jobs", runtime.NumCPU(), "endpoints to convert in parallel")
	f.BoolVar(&o.quiet, "quiet", false, "no progress counter")
	return cmd
}

// migrateOpts is one invocation of migrate.
type migrateOpts struct {
	baseDir string
	format  string
	set     string
	remove  bool
	dryRun  bool
	verbose bool
	quiet   bool
	jobs    int
}

// run converts every named endpoint, or the whole cache.
//
// The work parallelises because a shard is a shard's own business: each one has
// its own lock, its own index and its own segments, so two conversions share
// nothing but the disk. What they do share is the counter and the two output
// streams, and those go through one progress, which is why the results are
// collected here rather than reported from the workers.
func (o *migrateOpts) run(ctx context.Context, args []string) error {
	targets := migrateTargets(o.baseDir, o.format, o.set, args)
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to migrate")
		return nil
	}
	if o.dryRun {
		return o.reportDryRun(targets)
	}
	jobs := max(o.jobs, 1)
	p := newProgress(os.Stderr, os.Stdout, StderrIsTerminal(), o.quiet, "migrating", len(targets))
	defer p.stop()

	work := make(chan store.Identity)
	results := make(chan migrateOutcome)
	var wg sync.WaitGroup
	for range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range work {
				p.begin(id.BaseURL)
				results <- migrateOne(o.baseDir, id, o.remove)
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
	}()

	var converted, current, kept, failed int
	for out := range results {
		switch {
		case out.err != nil:
			failed++
			p.printf("%s: %v", out.id.BaseURL, out.err)
		case out.kept != nil:
			kept++
			p.printf("%s: source kept: %v", out.id.BaseURL, out.kept)
		case out.wrote:
			converted++
		default:
			// Migrated by an earlier run: not a failure, and not work done
			// either. Counting it apart is what makes "metha migrate --rm"
			// after a plain "metha migrate" read as the second step it is.
			current++
		}
		if out.err == nil && !o.remove && len(out.result.Skipped) > 0 {
			p.printf("%s: %s", out.id.BaseURL, skippedFiles(out.result.Skipped))
		}
		if o.verbose && out.result != nil {
			r := out.result
			p.dataf("%s\t%s\t%s\t%d windows\t%d requests\t%d records\t%d in shard",
				out.id.BaseURL, out.id.Format, out.id.Set, r.Windows, r.Requests, r.Source, r.Records)
		}
		p.step(bytesOf(out.result), out.err != nil)
	}
	p.stop()

	fmt.Fprintf(os.Stderr, "%d converted", converted)
	if current > 0 {
		fmt.Fprintf(os.Stderr, ", %d already up to date", current)
	}
	if kept > 0 {
		fmt.Fprintf(os.Stderr, ", %d source %s kept", kept, plural(kept, "directory"))
	}
	fmt.Fprintf(os.Stderr, ", %d failed\n", failed)
	if kept > 0 {
		fmt.Fprintf(os.Stderr, "the data is in the shards either way; each source above stays until what it names is dealt with\n")
	}
	// An interrupt leaves the rest of the cache unconverted, which is a normal
	// way to stop a run this long and still not a run that did what was asked.
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted after %d of %d %s", converted+current+kept+failed,
			len(targets), plural(len(targets), "endpoint"))
	}
	if failed > 0 {
		return fmt.Errorf("%d %s failed to convert; no source was removed for them",
			failed, plural(failed, "endpoint"))
	}
	return nil
}

// skippedFiles is what to say about response files a migration read past. They
// are the one way a pre-1.0 directory can hold data that no shard does, which
// is why they are reported whether or not --rm was asked for.
func skippedFiles(skipped []string) string {
	return fmt.Sprintf("%d %s with no window date in the name, not migrated, first %s",
		len(skipped), plural(len(skipped), "file"), skipped[0])
}

// reportDryRun says what is there without touching it: what would be converted,
// how much of it there is, and which directories --rm would refuse to remove
// afterwards. Reading the sizes costs one readdir per endpoint, which is worth
// it for the one command whose whole job is to be run before the real one.
func (o *migrateOpts) reportDryRun(targets []store.Identity) error {
	var files int
	var bytes int64
	for _, id := range targets {
		c, err := store.ReadLegacyDir(o.baseDir, id)
		if err != nil {
			log.Printf("%s: %v", id.BaseURL, err)
			continue
		}
		files += len(c.Data)
		bytes += c.Bytes
		fmt.Printf("would migrate\t%s\t%s\t%s\t%d %s\t%s\n",
			id.BaseURL, id.Format, dash(id.Set), len(c.Data), plural(len(c.Data), "file"), humanBytes(c.Bytes))
		if left := c.Unconverted(); len(left) > 0 {
			fmt.Fprintf(os.Stderr, "%s: --rm would keep this directory: %v\n", id.BaseURL, left)
		}
	}
	fmt.Fprintf(os.Stderr, "%d %s, %d %s, %s\n", len(targets), plural(len(targets), "endpoint"),
		files, plural(files, "file"), humanBytes(bytes))
	return nil
}

// migrateTargets returns the identities to migrate: the endpoints named on the
// command line, or every pre-1.0 directory in the cache.
func migrateTargets(baseDir, format, set string, args []string) []store.Identity {
	var ids []store.Identity
	if len(args) > 0 {
		for _, arg := range args {
			ids = append(ids, store.Identity{
				BaseURL: oai.PrependSchema(arg),
				Format:  format,
				Set:     set,
			})
		}
		return ids
	}
	for id, err := range store.ListLegacy(baseDir) {
		if err != nil {
			log.Printf("skipping: %v", err)
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// migrateOutcome is what became of one endpoint. The two errors are different
// answers and are counted apart: err means the data is not converted, kept
// means it is - verified, in the shard - and only the source directory is
// still there, which costs disk and loses nothing.
type migrateOutcome struct {
	id     store.Identity
	result *store.MigrateResult
	wrote  bool
	err    error
	kept   error
}

// migrateOne converts one endpoint and, with remove, removes the source it came
// from - but only once three things hold: the conversion finished, the record
// counts on both sides agree, and every file in the directory was accounted
// for. Anything else leaves the directory exactly as it was and says why.
func migrateOne(baseDir string, id store.Identity, remove bool) migrateOutcome {
	out := migrateOutcome{id: id}
	result, err := store.Migrate(baseDir, id)
	if err != nil {
		out.err = err
		return out
	}
	out.result = result
	out.wrote = result.Windows > 0
	if !result.Verified() {
		out.err = fmt.Errorf("verification failed: %d records in the source files, %d in the shard",
			result.Source, result.Present)
		return out
	}
	if !remove {
		return out
	}
	// Verification cannot see these. It compares what was counted, and a
	// skipped file was never read, so its records are in neither number - a
	// migration that drops one still verifies. Removing the directory would
	// take those files with it, unmigrated, with nothing left to migrate from.
	if len(result.Skipped) > 0 {
		out.kept = errors.New(skippedFiles(result.Skipped))
		return out
	}
	// And the store refuses over anything else in there - a note, a
	// subdirectory, a file this version does not recognise.
	if err := store.RemoveLegacy(baseDir, id); err != nil {
		out.kept = err
	}
	return out
}

// bytesOf is what an outcome moved, for the counter; an endpoint that failed or
// was already converted moved nothing.
func bytesOf(result *store.MigrateResult) int64 {
	if result == nil {
		return 0
	}
	return result.Bytes
}
