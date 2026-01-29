package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/xflag"
	log "github.com/sirupsen/logrus"
)

var (
	baseDir                    = flag.String("base-dir", metha.GetBaseDir(), "base dir for harvested files")
	hourly                     = flag.Bool("hourly", false, "use hourly intervals for harvesting")
	daily                      = flag.Bool("daily", false, "use daily intervals for harvesting")
	delay                      = flag.Duration("delay", 0, "sleep between each OAI-PMH request")
	disableSelectiveHarvesting = flag.Bool("no-intervals", false, "harvest in one go, for funny endpoints")
	endpointList               = flag.Bool("list", false, "list a selection of OAI endpoints (might be outdated)")
	format                     = flag.String("format", "oai_dc", "metadata format")
	from                       = flag.String("from", "", "set the start date, format: 2006-01-02, use only if you do not want the endpoints earliest date")
	ignoreHTTPErrors           = flag.Bool("ignore-http-errors", false, "do not stop on HTTP errors, just skip to the next interval")
	logFile                    = flag.String("log", "", "filename to log to")
	logStderr                  = flag.Bool("log-errors-to-stderr", false, "Log errors and warnings to STDERR. If -log or -q are not given, write full log to STDOUT")
	maxEmptyReponses           = flag.Int("max-empty-responses", 10, "allow a number of empty responses before failing")
	maxRequests                = flag.Int("max", 1048576, "maximum number of token loops")
	quiet                      = flag.Bool("q", false, "suppress all output")
	removeCached               = flag.Bool("rm", false, "remove all cached files before starting anew")
	set                        = flag.String("set", "", "set name")
	showDir                    = flag.Bool("dir", false, "show target directory")
	suppressFormatParameter    = flag.Bool("suppress-format-parameter", false, "do not send format parameter")
	until                      = flag.String("until", "", "set the end date, format: 2006-01-02, use only if you do not want got records till today")
	version                    = flag.Bool("v", false, "show version")
	basicAuthCreds             = flag.String("u", "", "basic auth, like: user:password")
	extraHeaders               xflag.Array // Extra HTTP header.
	timeout                    = flag.Duration("T", 30*time.Second, "http client timeout")
	maxRetries                 = flag.Int("r", 10, "max number of retries")
	keepTemporaryFiles         = flag.Bool("k", false, "keep temporary files when interrupted")
	ignoreUnexpectedEOF        = flag.Bool("ignore-unexpected-eof", false, "ignore unexpected EOF")
	rateLimit                  = flag.String("rate-limit", "", "download rate limit (e.g., '1MB', '500KB', '2.5MB/s', '1024'). If no unit specified, bytes/sec assumed. Set to 0 or empty to disable")
)

// parseRateLimit converts a human-readable rate limit string to bytes per second
func parseRateLimit(input string) (float64, error) {
	if input == "" || input == "0" {
		return 0, nil
	}

	// Remove '/s' suffix if present (e.g., "1MB/s" -> "1MB")
	input = strings.TrimSuffix(strings.ToUpper(input), "/S")

	// Check for unit suffixes
	multiplier := 1.0
	var numStr string

	if strings.HasSuffix(input, "KB") {
		multiplier = 1024
		numStr = strings.TrimSuffix(input, "KB")
	} else if strings.HasSuffix(input, "MB") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(input, "MB")
	} else if strings.HasSuffix(input, "GB") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(input, "GB")
	} else if strings.HasSuffix(input, "K") {
		multiplier = 1024
		numStr = strings.TrimSuffix(input, "K")
	} else if strings.HasSuffix(input, "M") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(input, "M")
	} else if strings.HasSuffix(input, "G") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(input, "G")
	} else {
		// No unit, assume bytes
		numStr = input
	}

	rate, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate limit format: %s", input)
	}

	return rate * multiplier, nil
}

func main() {
	rand.Seed(time.Now().UnixNano())
	flag.Var(&extraHeaders, "H", `extra HTTP header to pass to requests (repeatable); e.g. -H "token: 123" `)
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

	// Parse rate limit
	rateLimitBytesPerSec, err := parseRateLimit(*rateLimit)
	if err != nil {
		log.Fatalf("Invalid rate limit: %v", err)
	}
	if rateLimitBytesPerSec > 0 {
		log.Printf("Rate limiting enabled: %.2f bytes/sec (%.2f KB/s)",
			rateLimitBytesPerSec, rateLimitBytesPerSec/1024)
	}

	metha.BaseDir = *baseDir
	baseURL := metha.PrependSchema(flag.Arg(0))
	if *showDir {
		harvest := metha.Harvest{Config: &metha.Config{
			BaseURL: baseURL,
			Format:  *format,
			Set:     *set,
		}}
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
	if *logStderr {
		if !*quiet && *logFile == "" {
			log.Warn(`The default logger writes to STDERR. Writing errors there was explicitly requested, but -q or -log were not specified. Writing entire log to STDOUT to avoid double-writing error messages.`)
			log.SetOutput(os.Stdout)
		}

		log.AddHook(metha.NewCopyHook(os.Stderr))
	}
	if *basicAuthCreds != "" {
		parts := strings.Split(*basicAuthCreds, ":")
		if len(parts) != 2 {
			log.Fatal("invalid format, we require username:password")
		}
		extraHeaders.Set("Authorization: Basic " + basicAuth(parts[0], parts[1]))
	}
	var extra = make(http.Header)
	for _, s := range extraHeaders {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			log.Fatalf(`extra headers notation is "Some-Key: Some-Value", got %v`, parts)
		}
		extra.Set(parts[0], parts[1])
	}
	harvest, err := metha.NewHarvest(baseURL)
	if err != nil {
		log.Fatal(err)
	}
	// if the harvest resulted in any extra header set, add them here
	if harvest.Config.ExtraHeaders != nil {
		for k, vs := range harvest.Config.ExtraHeaders {
			for _, v := range vs {
				extra.Add(k, v)
			}
		}
	}

	// Create client with rate limiting support
	if rateLimitBytesPerSec > 0 {
		harvest.Client = metha.CreateClientWithRateLimit(*timeout, *maxRetries, rateLimitBytesPerSec)
	} else {
		harvest.Client = metha.CreateClient(*timeout, *maxRetries)
	}

	harvest.Config.From = *from
	harvest.Config.Until = *until
	harvest.Config.Format = *format
	harvest.Config.Set = *set
	harvest.Config.MaxRequests = *maxRequests
	harvest.Config.CleanBeforeDecode = true
	harvest.Config.DisableSelectiveHarvesting = *disableSelectiveHarvesting
	harvest.Config.MaxEmptyResponses = *maxEmptyReponses
	harvest.Config.IgnoreHTTPErrors = *ignoreHTTPErrors
	harvest.Config.SuppressFormatParameter = *suppressFormatParameter
	harvest.Config.HourlyInterval = *hourly
	harvest.Config.DailyInterval = *daily
	harvest.Config.ExtraHeaders = extra
	harvest.Config.Delay = *delay
	harvest.Config.KeepTemporaryFiles = *keepTemporaryFiles
	harvest.Config.IgnoreHTTPErrors = *ignoreUnexpectedEOF
	log.Printf("harvest: %+v", harvest)
	if *removeCached {
		log.Printf("removing already cached files from %s", harvest.Dir())
		if err := os.RemoveAll(harvest.Dir()); err != nil {
			log.Println(err)
		}
	}
	if err := harvest.Run(); err != nil {
		if errors.Is(err, metha.ErrAlreadySynced) {
			log.Println("this repository is up-to-date")
			return
		}
		log.Fatal(err)
	}
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
