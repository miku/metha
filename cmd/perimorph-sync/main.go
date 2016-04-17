package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/miku/perimorph"
)

func main() {

	format := flag.String("format", "oai_dc", "metadata format")
	set := flag.String("set", "", "set name")
	showDir := flag.Bool("dir", false, "show target directory")
	maxRequests := flag.Int("max", 65536, "maximum number of token loops")
	disableSelectiveHarvesting := flag.Bool("disable-selective", false, "no intervals")
	version := flag.Bool("v", false, "show version")

	flag.Parse()

	if *version {
		fmt.Println(perimorph.Version)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		log.Fatal("endpoint required")
	}

	harvest, err := perimorph.NewHarvest(perimorph.PrependSchema(flag.Arg(0)))
	if err != nil {
		log.Fatal(err)
	}

	harvest.Format = *format
	harvest.Set = *set
	harvest.MaxRequests = *maxRequests
	harvest.CleanBeforeDecode = true
	harvest.DisableSelectiveHarvesting = *disableSelectiveHarvesting
	harvest.MaxEmptyResponses = 10

	if *showDir {
		fmt.Println(harvest.Dir())
		os.Exit(0)
	}

	log.Printf("harvest: %+v", harvest)

	if err := harvest.Run(); err != nil {
		log.Fatal(err)
	}
	flag.Parse()
}
