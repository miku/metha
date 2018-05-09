package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/miku/metha"
)

func main() {
	format := flag.String("format", "oai_dc", "metadata format")
	set := flag.String("set", "", "set name")
	version := flag.Bool("v", false, "show version")

	flag.Parse()

	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		log.Fatal("endpoint required")
	}

	harvest := metha.Harvest{
		BaseURL: metha.PrependSchema(flag.Arg(0)),
		Format:  *format,
		Set:     *set,
	}

	for _, fn := range harvest.Files() {
		fmt.Println(fn)
	}
}
