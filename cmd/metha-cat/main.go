package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

var (
	format  = flag.String("format", "oai_dc", "metadata format")
	set     = flag.String("set", "", "set name")
	version = flag.Bool("v", false, "show version")
	baseDir = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	from    = flag.String("from", "", "ignore records before this date")
	until   = flag.String("until", "", "ignore records after this date")
	root    = flag.String("root", "Records", "root element to wrap records into")
	useJson = flag.Bool("j", false, "output json, not xml")
)

func main() {
	flag.Parse()
	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}
	if flag.NArg() == 0 {
		log.Fatal("endpoint required")
	}
	baseURL := metha.PrependSchema(flag.Arg(0))
	metha.BaseDir = *baseDir
	harvest := metha.Harvest{Config: &metha.Config{
		BaseURL: baseURL,
		Format:  *format,
		Set:     *set,
	}}
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()
	opts := &metha.RenderOpts{
		Writer:  bw,
		Harvest: harvest,
		From:    *from,
		Until:   *until,
		Root:    *root,
		UseJson: *useJson,
	}
	if err := metha.Render(opts); err != nil {
		log.Fatal(err)
	}
}
