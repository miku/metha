package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

var (
	baseDir = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	format  = flag.String("format", "oai_dc", "metadata format")
	set     = flag.String("set", "", "set name")
	version = flag.Bool("v", false, "show version")
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

	metha.BaseDir = *baseDir
	harvest := metha.Harvest{
		BaseURL: metha.PrependSchema(flag.Arg(0)),
		Format:  *format,
		Set:     *set,
	}

	for _, fn := range harvest.Files() {
		fmt.Println(fn)
	}
}
