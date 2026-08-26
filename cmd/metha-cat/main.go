package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
)

var (
	format      = flag.String("format", "oai_dc", "metadata format")
	set         = flag.String("set", "", "set name")
	version     = flag.Bool("v", false, "show version")
	baseDir     = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	from        = flag.String("from", "", "ignore records before this date")
	until       = flag.String("until", "", "ignore records after this date")
	root        = flag.String("root", "Records", "root element to wrap records into")
	useJson     = flag.Bool("j", false, "output json, not xml")
	setSpec     = flag.String("setspec", "", "only records carrying this setSpec")
	deleted     = flag.Bool("deleted", false, "include records the endpoint marked deleted")
	onlyDeleted = flag.Bool("only-deleted", false, "emit only the records the endpoint marked deleted")
)

// deletedPolicy turns the two flags into a policy. Deleted records are
// suppressed unless asked for: they carry no metadata, and a consumer that
// wants the tombstones is asking a different question than one reading records.
func deletedPolicy() store.DeletedPolicy {
	switch {
	case *onlyDeleted:
		return store.DeletedOnly
	case *deleted:
		return store.DeletedKeep
	default:
		return store.DeletedSkip
	}
}

func main() {
	flag.Parse()
	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}
	if flag.NArg() == 0 {
		log.Fatal("endpoint required")
	}
	st, err := store.Open(*baseDir, store.Identity{
		BaseURL: metha.PrependSchema(flag.Arg(0)),
		Format:  *format,
		Set:     *set,
	})
	if err != nil {
		log.Fatal(err)
	}
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	opts := store.RenderOpts{
		Writer:  bw,
		From:    *from,
		Until:   *until,
		SetSpec: *setSpec,
		Deleted: deletedPolicy(),
		Root:    *root,
		UseJson: *useJson,
	}
	if err := store.Render(st, opts); err != nil {
		log.Fatal(err)
	}
}
