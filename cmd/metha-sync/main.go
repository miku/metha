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
	showDir := flag.Bool("dir", false, "show target directory")
	maxRequests := flag.Int("max", 65536, "maximum number of token loops")
	disableSelectiveHarvesting := flag.Bool("no-intervals", false, "harvest in one go, for funny endpoints")
	ignoreHTTPErrors := flag.Bool("ignore-http-errors", false, "do not stop on HTTP errors, just skip to the next interval")
	suppressFormatParameter := flag.Bool("suppress-format-parameter", false, "do not send format parameter")
	version := flag.Bool("v", false, "show version")
	daily := flag.Bool("daily", false, "use daily intervals for harvesting")

	logFile := flag.String("log", "", "filename to log to")

	flag.Parse()

	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		log.Fatal("endpoint required")
	}

	baseURL := metha.PrependSchema(flag.Arg(0))

	if *showDir {
		// showDir only needs these parameters
		harvest := metha.Harvest{
			BaseURL: baseURL,
			Format:  *format,
			Set:     *set,
		}
		fmt.Println(harvest.Dir())
		os.Exit(0)
	}

	if *logFile != "" {
		file, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %s", err)
		}
		log.SetOutput(file)

	}

	// NewHarvest ensures the endpoint is sane, before we start
	harvest, err := metha.NewHarvest(baseURL)
	if err != nil {
		log.Fatal(err)
	}

	harvest.Format = *format
	harvest.Set = *set
	harvest.MaxRequests = *maxRequests
	harvest.CleanBeforeDecode = true
	harvest.DisableSelectiveHarvesting = *disableSelectiveHarvesting
	harvest.MaxEmptyResponses = 10
	harvest.IgnoreHTTPErrors = *ignoreHTTPErrors
	harvest.SuppressFormatParameter = *suppressFormatParameter
	harvest.DailyInterval = *daily

	log.Printf("harvest: %+v", harvest)

	if err := harvest.Run(); err != nil {
		if err == metha.ErrAlreadySynced {
			log.Println(err)
		} else {
			log.Fatal(err)
		}
	}
}
