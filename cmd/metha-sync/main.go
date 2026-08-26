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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
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
	noCompression              = flag.Bool("no-compression", false, "store harvested files as plain XML instead of .xml.gz or .xml.zst")
	layout                     = flag.String("layout", "", "storage layout: v1 (a directory of files) or v2 (a sharded, indexed store); default is the layout the endpoint already uses, or v1")
)

// harvestLayout decides where a harvest writes. An endpoint that already has a
// v2 shard keeps it, so a converted cache does not silently start writing files
// beside it again; otherwise -layout, then METHA_LAYOUT, then v1, which is what
// every existing installation has.
func harvestLayout(id store.Identity) store.Layout {
	if *layout != "" {
		return store.Layout(*layout)
	}
	if env := os.Getenv(store.LayoutEnv); env != "" {
		return store.Layout(env)
	}
	return store.Detect(*baseDir, id)
}

// noticeName marks a cache whose owner has been told about v2 already.
const noticeName = ".metha-v2-notice"

// noticeOnce tells the user once per cache that their v1 data can be
// converted, with the command that does it, and never mentions it again.
func noticeOnce(id store.Identity) {
	marker := filepath.Join(*baseDir, noticeName)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	src, err := store.OpenLayout(*baseDir, id, store.V1)
	if err != nil {
		return
	}
	if files, err := src.Files(); err != nil || len(files) == 0 {
		return // Nothing harvested here yet, so nothing to convert.
	}
	fmt.Fprintf(os.Stderr, `
A newer storage layout (v2) is available: one shard per endpoint, an index
instead of a file per window, and no file at all for windows that returned
nothing. Your harvested data can be converted in place, without refetching:

    metha-migrate -base-dir %s        # convert everything, keeping the originals
    metha-migrate -base-dir %s -rm    # and drop the originals once verified

Harvests continue to work unchanged in v1. This notice is shown once.

`, *baseDir, *baseDir)
	os.WriteFile(marker, nil, 0644)
}

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
	identity := store.Identity{BaseURL: baseURL, Format: *format, Set: *set}
	if *showDir {
		st, err := store.OpenLayout(*baseDir, identity, harvestLayout(identity))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(st.Dir())
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
	harvest.Config.IgnoreUnexpectedEOF = *ignoreUnexpectedEOF
	harvest.Config.NoCompression = *noCompression
	resolved := harvestLayout(identity)
	if *removeCached {
		log.Printf("removing already cached data for %s", identity.BaseURL)
		if err := store.Remove(*baseDir, identity, resolved); err != nil {
			log.Println(err)
		}
	}
	if err := runHarvest(harvest, identity, resolved); err != nil {
		switch {
		case errors.Is(err, metha.ErrAlreadySynced):
			log.Println("this repository is up-to-date")
			return
		case errors.Is(err, metha.ErrLocked):
			// Expected when the same endpoint is handed to two workers,
			// e.g. by the shuf | parallel loop in the README. Not a failure.
			log.Printf("another harvest holds this endpoint, skipping: %v", err)
			return
		}
		log.Fatal(err)
	}
}

// runHarvest points a harvest at the layout it should write into, and runs it.
//
// Every path out of here closes the sink, which is why it is a function and not
// part of main: log.Fatal exits the process without running deferred calls, and
// an index left open that way keeps its write-ahead log and shared-memory files
// on disk, with the last committed window still in the log rather than in the
// database.
func runHarvest(harvest *metha.Harvest, id store.Identity, layout store.Layout) error {
	switch layout {
	case store.V1:
		noticeOnce(id)
	case store.V2:
		w, err := store.OpenWriter(*baseDir, id)
		if err != nil {
			return err
		}
		defer w.Close()
		// The identify response is what a shard needs to be self-describing,
		// and v1 had nowhere to keep it.
		if err := w.SetIdentify(harvest.Identify); err != nil {
			return err
		}
		harvest.Sink = w
	default:
		return fmt.Errorf("unknown layout: %v, use v1 or v2", layout)
	}
	log.Printf("harvest: %+v", harvest)
	return harvest.Run()
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
