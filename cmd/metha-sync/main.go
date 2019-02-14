package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/miku/metha"
	log "github.com/sirupsen/logrus"
)

func getBaseDir() string {
	if dir := os.Getenv("METHA_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(metha.UserHomeDir(), ".metha")
}

func main() {

	format := flag.String("format", "oai_dc", "metadata format")
	set := flag.String("set", "", "set name")
	showDir := flag.Bool("dir", false, "show target directory")
	baseDir := flag.String("base-dir", getBaseDir(), "base dir for harvested files")
	maxRequests := flag.Int("max", 1048576, "maximum number of token loops")
	disableSelectiveHarvesting := flag.Bool("no-intervals", false, "harvest in one go, for funny endpoints")
	ignoreHTTPErrors := flag.Bool("ignore-http-errors", false, "do not stop on HTTP errors, just skip to the next interval")
	suppressFormatParameter := flag.Bool("suppress-format-parameter", false, "do not send format parameter")
	version := flag.Bool("v", false, "show version")
	daily := flag.Bool("daily", false, "use daily intervals for harvesting")
	from := flag.String("from", "", "set the start date, format: 2006-01-02, use only if you do not want the endpoints earliest date")
	quiet := flag.Bool("q", false, "suppress all output")
	endpointList := flag.Bool("list", false, "list a selection of OAI endpoints (might be outdated)")
	maxEmptyReponses := flag.Int("max-empty-responses", 10, "allow a number of empty responses before failing")

	logFile := flag.String("log", "", "filename to log to")

	flag.Parse()

	if *version {
		fmt.Println(metha.Version)
		os.Exit(0)
	}

	if *endpointList {
		for _, u := range metha.Endpoints {
			fmt.Println(u)
		}
		os.Exit(0)
	}

	if flag.NArg() == 0 {
		log.Fatalf("An endpoint URL is required, maybe try: %s", metha.RandomEndpoint())
	}

	metha.BaseDir = *baseDir
	baseURL := metha.PrependSchema(flag.Arg(0))

	if *showDir {
		harvest := metha.Harvest{
			BaseURL: baseURL,
			Format:  *format,
			Set:     *set,
		}
		fmt.Println(harvest.Dir())
		os.Exit(0)
	}

	if *quiet {
		log.SetOutput(ioutil.Discard)
	}

	if *logFile != "" {
		file, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %s", err)
		}
		log.SetOutput(file)
	}

	harvest, err := metha.NewHarvest(baseURL)
	if err != nil {
		log.Fatal(err)
	}

	harvest.From = *from
	harvest.Format = *format
	harvest.Set = *set
	harvest.MaxRequests = *maxRequests
	harvest.CleanBeforeDecode = true
	harvest.DisableSelectiveHarvesting = *disableSelectiveHarvesting
	harvest.MaxEmptyResponses = *maxEmptyReponses
	harvest.IgnoreHTTPErrors = *ignoreHTTPErrors
	harvest.SuppressFormatParameter = *suppressFormatParameter
	harvest.DailyInterval = *daily

	log.Printf("harvest: %+v", harvest)

	if err := harvest.Run(); err != nil {
		if err == metha.ErrAlreadySynced {
			log.Println("this repository is up-to date")
		} else {
			log.Fatal(err)
		}
	}
}
