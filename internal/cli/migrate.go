package cli

import (
	"fmt"
	"os"

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
// given, and only ever removed after the record counts have been checked.
func newMigrateCmd() *cobra.Command {
	var (
		baseDir string
		format  string
		set     string
		remove  bool
		dryRun  bool
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "migrate [ENDPOINT...]",
		Short: "Convert pre-1.0 directories into shards",
		Example: `  metha migrate                          # every endpoint in the cache
  metha migrate http://export.arxiv.org/oai2
  metha migrate --dry-run --verbose      # what would happen
  metha migrate --rm                     # convert, then drop the old dirs`,
		Aliases: []string{"metha-migrate"},
		RunE: func(cmd *cobra.Command, args []string) error {
			targets := migrateTargets(baseDir, format, set, args)
			if len(targets) == 0 {
				fmt.Fprintln(os.Stderr, "nothing to migrate")
				return nil
			}
			var converted, current, failed int
			for _, id := range targets {
				if dryRun {
					fmt.Printf("would migrate\t%s\t%s\t%s\n", id.BaseURL, id.Format, id.Set)
					converted++
					continue
				}
				wrote, err := migrateOne(baseDir, id, remove, verbose)
				if err != nil {
					log.Printf("%s: %v", id.BaseURL, err)
					failed++
					continue
				}
				// An endpoint migrated by an earlier run is not a failure and
				// not work done either; counting it apart is what makes "metha
				// migrate --rm" after a plain "metha migrate" read as the
				// second step it is.
				if wrote {
					converted++
				} else {
					current++
				}
			}
			fmt.Fprintf(os.Stderr, "%d converted", converted)
			if current > 0 {
				fmt.Fprintf(os.Stderr, ", %d already up to date", current)
			}
			fmt.Fprintf(os.Stderr, ", %d failed\n", failed)
			if failed > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.StringVar(&format, "format", "oai_dc", "metadata format, when an endpoint is named")
	f.StringVar(&set, "set", "", "set name, when an endpoint is named")
	f.BoolVar(&remove, "rm", false, "remove the old directory after a verified migration")
	f.BoolVar(&dryRun, "dry-run", false, "only report what would be migrated")
	f.BoolVar(&verbose, "verbose", false, "report every endpoint, not just failures")
	return cmd
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

// migrateOne converts one endpoint, reporting whether it had anything to write.
func migrateOne(baseDir string, id store.Identity, remove, verbose bool) (bool, error) {
	result, err := store.Migrate(baseDir, id)
	if err != nil {
		return false, err
	}
	for _, file := range result.Skipped {
		log.Printf("skipped, no window date in the filename: %s", file)
	}
	wrote := result.Windows > 0
	if !result.Verified() {
		return wrote, fmt.Errorf("verification failed: %d records in the source files, %d in the shard",
			result.Source, result.Present)
	}
	if verbose {
		fmt.Printf("%s\t%s\t%s\t%d windows\t%d requests\t%d records\t%d in shard\n",
			id.BaseURL, id.Format, id.Set, result.Windows, result.Requests, result.Source, result.Records)
	}
	if !remove {
		return wrote, nil
	}
	// Verification cannot see these. It compares what was counted, and a
	// skipped file was never read, so its records are in neither number - a
	// migration that drops one still verifies. Removing the directory would
	// take those files with it, unmigrated, with nothing left to migrate from.
	if len(result.Skipped) > 0 {
		return wrote, fmt.Errorf("refusing to remove the source directory: %d file(s) carry no window date and were not migrated, first %s",
			len(result.Skipped), result.Skipped[0])
	}
	// Only ever after the counts match, and only the directory this identity
	// owns - a pre-1.0 directory holds exactly one format and set.
	return wrote, store.RemoveLegacy(baseDir, id)
}
