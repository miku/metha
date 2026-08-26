// metha-migrate converts harvested data from the v1 layout, a directory of
// compressed responses per endpoint, into v2 shards.
//
// No re-harvest is needed: a v1 file holds complete responses, so a shard can
// be built from the cache alone. The source is left in place unless -rm is
// given, and only ever removed after the record counts have been checked.
//
//	metha-migrate                          # every endpoint in the cache
//	metha-migrate http://export.arxiv.org/oai2
//	metha-migrate -dry-run -v              # what would happen
//	metha-migrate -rm                      # convert, then drop the v1 dirs
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
)

var (
	baseDir = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	format  = flag.String("format", "oai_dc", "metadata format, when an endpoint is named")
	set     = flag.String("set", "", "set name, when an endpoint is named")
	remove  = flag.Bool("rm", false, "remove the v1 directory after a verified migration")
	dryRun  = flag.Bool("dry-run", false, "only report what would be migrated")
	verbose = flag.Bool("v", false, "report every endpoint, not just failures")
	version = flag.Bool("version", false, "show version")
)

func main() {
	flag.Parse()
	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}
	targets, err := targets()
	if err != nil {
		log.Fatal(err)
	}
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to migrate")
		return
	}
	var migrated, failed int
	for _, id := range targets {
		if *dryRun {
			fmt.Printf("would migrate\t%s\t%s\t%s\n", id.BaseURL, id.Format, id.Set)
			migrated++
			continue
		}
		if err := migrate(id); err != nil {
			log.Printf("%s: %v", id.BaseURL, err)
			failed++
			continue
		}
		migrated++
	}
	fmt.Fprintf(os.Stderr, "%d migrated, %d failed\n", migrated, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// targets returns the identities to migrate: the endpoints named on the
// command line, or every v1 directory in the cache.
func targets() ([]store.Identity, error) {
	if flag.NArg() > 0 {
		var ids []store.Identity
		for _, arg := range flag.Args() {
			ids = append(ids, store.Identity{
				BaseURL: metha.PrependSchema(arg),
				Format:  *format,
				Set:     *set,
			})
		}
		return ids, nil
	}
	var ids []store.Identity
	for entry, err := range store.List(*baseDir) {
		if err != nil {
			log.Printf("skipping: %v", err)
			continue
		}
		if entry.Layout == store.V1 {
			ids = append(ids, entry.Identity)
		}
	}
	return ids, nil
}

// migrate converts one endpoint and reports what it moved.
func migrate(id store.Identity) error {
	result, err := store.Migrate(*baseDir, id)
	if err != nil {
		return err
	}
	for _, file := range result.Skipped {
		log.Printf("skipped, no window date in the filename: %s", file)
	}
	if !result.Verified() {
		return fmt.Errorf("verification failed: %d records indexed, %d read from %d requests",
			result.Records, result.Appended, result.Requests)
	}
	if *verbose {
		fmt.Printf("%s\t%s\t%s\t%d windows\t%d requests\t%d records\n",
			id.BaseURL, id.Format, id.Set, result.Windows, result.Requests, result.Records)
	}
	if !*remove {
		return nil
	}
	// Only ever after the counts match, and only the directory this identity
	// owns - a v1 directory holds exactly one format and set.
	src, err := store.OpenLayout(*baseDir, id, store.V1)
	if err != nil {
		return err
	}
	if filepath.Dir(src.Dir()) != filepath.Clean(*baseDir) {
		return fmt.Errorf("refusing to remove %s: not directly under %s", src.Dir(), *baseDir)
	}
	return os.RemoveAll(src.Dir())
}
