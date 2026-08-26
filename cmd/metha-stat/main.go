// metha-stat reports what the cache holds and what a harvest cost.
//
// With an endpoint, it describes that one: windows harvested, how many returned
// nothing, records, tombstones, bytes on disk against bytes fetched, and the
// throughput the endpoint managed. Without one, it summarises the whole cache.
//
// The counts come from the v2 index. A v1 directory keeps no account of what it
// holds beyond its filenames, so those fields read "-".
//
//	metha-stat http://export.arxiv.org/oai2
//	metha-stat                                 # the whole cache
//	metha-stat -json                           # for further processing
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
)

var (
	baseDir  = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	format   = flag.String("format", "oai_dc", "metadata format, when an endpoint is named")
	set      = flag.String("set", "", "set name, when an endpoint is named")
	asJson   = flag.Bool("json", false, "emit json")
	version  = flag.Bool("v", false, "show version")
	failures = flag.Bool("failed", false, "list only endpoints with failed windows")
)

func main() {
	flag.Parse()
	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}
	if flag.NArg() > 0 {
		if err := statEndpoints(flag.Args()); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := statCache(); err != nil {
		log.Fatal(err)
	}
}

// statEndpoints reports on the endpoints named on the command line.
func statEndpoints(args []string) error {
	for _, arg := range args {
		id := store.Identity{BaseURL: metha.PrependSchema(arg), Format: *format, Set: *set}
		stats, err := store.Stat(*baseDir, id)
		if err != nil {
			return err
		}
		if *asJson {
			if err := writeJSON(stats); err != nil {
				return err
			}
			continue
		}
		report(stats)
	}
	return nil
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
	fmt.Fprintf(tw, "layout\t%s\n", s.Layout)
	if s.Superseded {
		fmt.Fprintf(tw, "\tmigrated to v2 already; this directory is a leftover copy\n")
	}
	if s.StaleV1 != "" {
		fmt.Fprintf(tw, "stale v1\t%s\n", s.StaleV1)
	}
	fmt.Fprintf(tw, "files\t%d\n", s.Files)
	fmt.Fprintf(tw, "size\t%s\n", humanBytes(s.Bytes))
	fmt.Fprintf(tw, "windows\t%s", count(s.Windows))
	if s.Empty != store.Unknown {
		fmt.Fprintf(tw, " (%s empty, %s failed)", count(s.Empty), count(s.Failed))
	}
	fmt.Fprintln(tw)
	fmt.Fprintf(tw, "requests\t%s\n", count(s.Requests))
	fmt.Fprintf(tw, "records\t%s", count(s.Records))
	if s.Deleted != store.Unknown {
		fmt.Fprintf(tw, " (%s deleted)", count(s.Deleted))
	}
	fmt.Fprintln(tw)
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
func statCache() error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	defer tw.Flush()
	if !*asJson {
		fmt.Fprintln(tw, "LAYOUT\tSIZE\tWINDOWS\tRECORDS\tDELETED\tFAILED\tLAST\tENDPOINT")
	}
	var (
		total      store.Stats
		shards     int
		v1, v2     int
		superseded int
		skipped    int
	)
	for entry, err := range store.List(*baseDir) {
		if err != nil {
			log.Printf("skipping: %v", err)
			skipped++
			continue
		}
		// The entry's own layout, not a detected one: a migration that kept
		// its source leaves the same identity in both layouts, and each copy
		// gets the line that describes it.
		stats, err := store.StatLayout(*baseDir, entry.Identity, entry.Layout)
		if err != nil {
			log.Printf("%s: %v", entry.Identity.BaseURL, err)
			skipped++
			continue
		}
		if *failures && stats.Failed <= 0 {
			continue
		}
		shards++
		switch stats.Layout {
		case store.V1:
			v1++
		case store.V2:
			v2++
		}
		if stats.Superseded {
			superseded++
		}
		total.Bytes += stats.Bytes
		total.Files += stats.Files
		for _, p := range []struct {
			into *int
			from int
		}{
			{&total.Windows, stats.Windows}, {&total.Empty, stats.Empty},
			{&total.Failed, stats.Failed}, {&total.Requests, stats.Requests},
			{&total.Records, stats.Records}, {&total.Deleted, stats.Deleted},
		} {
			if p.from != store.Unknown {
				*p.into += p.from
			}
		}
		if stats.Fetched != store.Unknown {
			total.Fetched += stats.Fetched
		}
		total.Elapsed += stats.Elapsed
		if *asJson {
			if err := writeJSON(stats); err != nil {
				return err
			}
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			layout(stats), humanBytes(stats.Bytes), count(stats.Windows), count(stats.Records),
			count(stats.Deleted), count(stats.Failed), dash(stats.Last), stats.Identity.BaseURL)
	}
	if *asJson {
		return nil
	}
	tw.Flush()
	fmt.Fprintf(os.Stderr, "\n%d %s (%d v1, %d v2), %s on disk, %d records, %d deleted, %d failed windows",
		shards, plural(shards, "entry"), v1, v2, humanBytes(total.Bytes), total.Records, total.Deleted, total.Failed)
	if total.Fetched > 0 {
		fmt.Fprintf(os.Stderr, ", %s fetched", humanBytes(total.Fetched))
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, ", %d skipped", skipped)
	}
	fmt.Fprintln(os.Stderr)
	if superseded > 0 {
		fmt.Fprintf(os.Stderr, "%d v1 %s already migrated (*), still on disk: remove with metha-migrate -rm\n",
			superseded, plural(superseded, "directory"))
	}
	return nil
}

// layout renders the layout column, marking a v1 directory that has already
// been migrated: it is why the same endpoint can appear twice.
func layout(s *store.Stats) string {
	if s.Superseded {
		return string(s.Layout) + "*"
	}
	return string(s.Layout)
}

// writeJSON emits one object per line, for piping onward.
func writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", b)
	return nil
}

// count renders a number the layout may not know.
func count(n int) string {
	if n == store.Unknown {
		return "-"
	}
	return fmt.Sprintf("%d", n)
}

// duration renders a span at a precision that keeps it meaningful: rounding a
// fast harvest to the second prints "0s" beside a rate of megabytes per second.
func duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(10 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

// plural gives a count its noun.
func plural(n int, noun string) string {
	switch {
	case n == 1:
		return noun
	case strings.HasSuffix(noun, "y"):
		return noun[:len(noun)-1] + "ies"
	default:
		return noun + "s"
	}
}

// dash renders an empty string as a dash, so columns stay readable.
func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// humanBytes renders a byte count in units a person can read.
func humanBytes(n int64) string {
	if n == store.Unknown {
		return "-"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
