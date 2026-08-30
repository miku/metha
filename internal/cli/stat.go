package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// newStatCmd reports what the cache holds and what a harvest cost.
//
// With an endpoint, it describes that one: windows harvested, how many returned
// nothing, records, tombstones, bytes on disk against bytes fetched, and the
// throughput the endpoint managed. Without one, it summarises the whole cache.
func newStatCmd() *cobra.Command {
	var (
		baseDir  string
		format   string
		set      string
		asJSON   bool
		failures bool
	)
	cmd := &cobra.Command{
		Use:   "stat [ENDPOINT...]",
		Short: "Report what the cache holds and what a harvest cost",
		Example: `  metha stat http://export.arxiv.org/oai2
  metha stat                 # the whole cache
  metha stat --json          # for further processing`,
		Aliases: []string{"metha-stat"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				for _, arg := range args {
					id := store.Identity{BaseURL: oai.PrependSchema(arg), Format: format, Set: set}
					stats, err := store.Stat(baseDir, id)
					if err != nil {
						return err
					}
					if asJSON {
						if err := writeJSON(stats); err != nil {
							return err
						}
						continue
					}
					report(stats)
				}
				return nil
			}
			return statCache(baseDir, asJSON, failures)
		},
	}
	f := cmd.Flags()
	f.StringVar(&baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.StringVar(&format, "format", "oai_dc", "metadata format, when an endpoint is named")
	f.StringVar(&set, "set", "", "set name, when an endpoint is named")
	f.BoolVar(&asJSON, "json", false, "emit json")
	f.BoolVar(&failures, "failed", false, "list only endpoints with failed windows")
	return cmd
}

// report prints one endpoint in full.
func report(s *store.Stats) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintf(tw, "endpoint\t%s\n", s.Identity.BaseURL)
	fmt.Fprintf(tw, "format\t%s\n", s.Identity.Format)
	if s.Identity.Set != "" {
		fmt.Fprintf(tw, "set\t%s\n", s.Identity.Set)
	}
	fmt.Fprintf(tw, "files\t%d\n", s.Files)
	fmt.Fprintf(tw, "size\t%s\n", humanBytes(s.Bytes))
	fmt.Fprintf(tw, "windows\t%d (%d empty, %d failed)\n", s.Windows, s.Empty, s.Failed)
	fmt.Fprintf(tw, "requests\t%d\n", s.Requests)
	fmt.Fprintf(tw, "records\t%d (%d deleted)\n", s.Records, s.Deleted)
	if s.First != "" || s.Last != "" {
		fmt.Fprintf(tw, "covered\t%s .. %s\n", dash(s.First), dash(s.Last))
	}
	if !s.LastSeen.IsZero() {
		fmt.Fprintf(tw, "last harvest\t%s (%s ago)\n",
			s.LastSeen.Local().Format(time.RFC3339), time.Since(s.LastSeen).Round(time.Minute))
	}
	if s.Fetched > 0 {
		fmt.Fprintf(tw, "fetched\t%s\n", humanBytes(s.Fetched))
	}
	if r := s.Ratio(); r > 0 {
		fmt.Fprintf(tw, "compression\t%.1fx\n", r)
	}
	if s.Elapsed > 0 {
		fmt.Fprintf(tw, "harvest time\t%s\n", duration(s.Elapsed))
	}
	if rate := s.Rate(); rate > 0 {
		fmt.Fprintf(tw, "rate\t%s/s\n", humanBytes(int64(rate)))
	}
}

// statCache walks the whole cache, one line per endpoint, and totals it.
func statCache(baseDir string, asJSON, failures bool) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	defer tw.Flush()
	if !asJSON {
		fmt.Fprintln(tw, "SIZE\tWINDOWS\tRECORDS\tDELETED\tFAILED\tLAST\tENDPOINT")
	}
	var (
		total   store.Stats
		shards  int
		skipped int
	)
	for entry, err := range store.List(baseDir) {
		if err != nil {
			log.Printf("skipping: %v", err)
			skipped++
			continue
		}
		stats, err := store.Stat(baseDir, entry.Identity)
		if err != nil {
			log.Printf("%s: %v", entry.Identity.BaseURL, err)
			skipped++
			continue
		}
		if failures && stats.Failed <= 0 {
			continue
		}
		shards++
		total.Bytes += stats.Bytes
		total.Files += stats.Files
		total.Windows += stats.Windows
		total.Empty += stats.Empty
		total.Failed += stats.Failed
		total.Requests += stats.Requests
		total.Records += stats.Records
		total.Deleted += stats.Deleted
		total.Fetched += stats.Fetched
		total.Elapsed += stats.Elapsed
		if asJSON {
			if err := writeJSON(stats); err != nil {
				return err
			}
			continue
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\t%s\n",
			humanBytes(stats.Bytes), stats.Windows, stats.Records,
			stats.Deleted, stats.Failed, dash(stats.Last), stats.Identity.BaseURL)
	}
	if asJSON {
		return nil
	}
	tw.Flush()
	fmt.Fprintf(os.Stderr, "\n%d %s, %s on disk, %d records, %d deleted, %d failed windows",
		shards, plural(shards, "entry"), humanBytes(total.Bytes), total.Records, total.Deleted, total.Failed)
	if total.Fetched > 0 {
		fmt.Fprintf(os.Stderr, ", %s fetched", humanBytes(total.Fetched))
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, ", %d skipped", skipped)
	}
	fmt.Fprintln(os.Stderr)
	legacyFooter(os.Stderr, baseDir)
	return nil
}
