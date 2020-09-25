// Download all known endpoints, generate a single JSON file.
package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

var (
	baseDir     = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	format      = flag.String("format", "oai_dc", "metadata format")
	maxRequests = flag.Int("max", 1048576, "maximum number of token loops")
	quiet       = flag.Bool("q", false, "suppress all output")
)

func main() {
	flag.Parse()
	var failed []string
	for i, u := range metha.Endpoints {
		log.Printf("%d/%d", i, len(metha.Endpoints))
		harvest, err := metha.NewHarvest(u)
		if err != nil {
			failed = append(failed, u)
			log.Printf("failed (init): %s", u)
			continue
		}
		harvest.MaxRequests = *maxRequests
		harvest.CleanBeforeDecode = true
		harvest.Format = *format
		if err := harvest.Run(); err != nil {
			switch err {
			case metha.ErrAlreadySynced:
				log.Println("this repository is up-to-date")
			default:
				harvest.DisableSelectiveHarvesting = true
				if err := harvest.Run(); err != nil {
					failed = append(failed, u)
					log.Printf("failed (harvest): %s", u)
					continue
				}
			}
		}
	}
	f, err := ioutil.TempFile("", "metha-snapshot-")
	if err != nil {
		for _, f := range failed {
			fmt.Println(f)
		}
		os.Exit(1)
	}
	defer f.Close()
	for _, u := range failed {
		if _, err := io.WriteString(f, u); err != nil {
			log.Println(err)
		}
	}
	log.Printf("wrote failed endpoints to %s", f.Name())
}
