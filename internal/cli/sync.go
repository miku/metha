package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// syncOpts holds what the sync flags are parsed into. It is a struct rather
// than a run of closure variables only because there are twenty eight of them,
// and the harvest wants most of them together.
type syncOpts struct {
	baseDir                    string
	hourly                     bool
	daily                      bool
	delay                      time.Duration
	disableSelectiveHarvesting bool
	endpointList               bool
	format                     string
	from                       string
	ignoreHTTPErrors           bool
	logFile                    string
	logStderr                  bool
	maxEmptyResponses          int
	maxRequests                int
	quiet                      bool
	removeCached               bool
	set                        string
	showDir                    bool
	suppressFormatParameter    bool
	until                      string
	basicAuthCreds             string
	timeout                    time.Duration
	maxRetries                 int
	keepTemporaryFiles         bool
	ignoreUnexpectedEOF        bool
	rateLimit                  string
	noCompression              bool
	layout                     string
	extraHeaders               []string
}

func newSyncCmd() *cobra.Command {
	var o syncOpts
	cmd := &cobra.Command{
		Use:     "sync ENDPOINT",
		Short:   "Harvest an endpoint incrementally",
		Aliases: []string{"metha-sync"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.endpointList {
				for _, u := range metha.Endpoints {
					fmt.Println(u)
				}
				os.Exit(0)
			}
			if len(args) == 0 {
				return fmt.Errorf("an endpoint URL is required, maybe try: %s", metha.RandomEndpoint())
			}
			return o.run(args[0])
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.BoolVar(&o.hourly, "hourly", false, "use hourly intervals for harvesting")
	f.BoolVar(&o.daily, "daily", false, "use daily intervals for harvesting")
	f.DurationVar(&o.delay, "delay", 0, "sleep between each OAI-PMH request")
	f.BoolVar(&o.disableSelectiveHarvesting, "no-intervals", false, "harvest in one go, for funny endpoints")
	f.BoolVar(&o.endpointList, "list", false, "list a selection of OAI endpoints (might be outdated)")
	f.StringVar(&o.format, "format", "oai_dc", "metadata format")
	f.StringVar(&o.from, "from", "", "set the start date, format: 2006-01-02, use only if you do not want the endpoints earliest date")
	f.BoolVar(&o.ignoreHTTPErrors, "ignore-http-errors", false, "do not stop on HTTP errors, just skip to the next interval")
	f.StringVar(&o.logFile, "log", "", "filename to log to")
	f.BoolVar(&o.logStderr, "log-errors-to-stderr", false, "log errors and warnings to STDERR; if --log or -q are not given, write full log to STDOUT")
	f.IntVar(&o.maxEmptyResponses, "max-empty-responses", 10, "allow a number of empty responses before failing")
	f.IntVar(&o.maxRequests, "max", 1048576, "maximum number of token loops")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "suppress all output")
	f.BoolVar(&o.removeCached, "rm", false, "remove all cached files before starting anew")
	f.StringVar(&o.set, "set", "", "set name")
	f.BoolVar(&o.showDir, "dir", false, "show target directory")
	f.BoolVar(&o.suppressFormatParameter, "suppress-format-parameter", false, "do not send format parameter")
	f.StringVar(&o.until, "until", "", "set the end date, format: 2006-01-02, use only if you do not want got records till today")
	f.StringVarP(&o.basicAuthCreds, "basic-auth", "u", "", "basic auth, like: user:password")
	f.DurationVarP(&o.timeout, "timeout", "T", 30*time.Second, "http client timeout")
	f.IntVarP(&o.maxRetries, "retries", "r", 10, "max number of retries")
	f.BoolVarP(&o.keepTemporaryFiles, "keep-temporary-files", "k", false, "keep temporary files when interrupted")
	f.BoolVar(&o.ignoreUnexpectedEOF, "ignore-unexpected-eof", false, "ignore unexpected EOF")
	f.StringVar(&o.rateLimit, "rate-limit", "", "download rate limit (e.g., '1MB', '500KB', '2.5MB/s', '1024'); if no unit specified, bytes/sec assumed; set to 0 or empty to disable")
	f.BoolVar(&o.noCompression, "no-compression", false, "store harvested files as plain XML instead of .xml.gz or .xml.zst")
	f.StringVar(&o.layout, "layout", "", "storage layout: v1 (a directory of files) or v2 (a sharded, indexed store); default is the layout the endpoint already uses, or v2")
	f.StringArrayVarP(&o.extraHeaders, "header", "H", nil, `extra HTTP header to pass to requests (repeatable); e.g. -H "token: 123"`)
	return cmd
}

func (o *syncOpts) run(endpoint string) error {
	rateLimitBytesPerSec, err := parseRateLimit(o.rateLimit)
	if err != nil {
		return fmt.Errorf("invalid rate limit: %w", err)
	}
	if rateLimitBytesPerSec > 0 {
		log.Printf("Rate limiting enabled: %.2f bytes/sec (%.2f KB/s)",
			rateLimitBytesPerSec, rateLimitBytesPerSec/1024)
	}
	metha.BaseDir = o.baseDir
	baseURL := metha.PrependSchema(endpoint)
	identity := store.Identity{BaseURL: baseURL, Format: o.format, Set: o.set}
	if o.showDir {
		st, err := store.OpenLayout(o.baseDir, identity, o.harvestLayout(identity))
		if err != nil {
			return err
		}
		fmt.Println(st.Dir())
		os.Exit(0)
	}
	if o.quiet {
		log.SetOutput(io.Discard)
	}
	if o.logFile != "" {
		file, err := os.OpenFile(o.logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("error opening log file: %w", err)
		}
		log.SetOutput(file)
	}
	if o.logStderr {
		if !o.quiet && o.logFile == "" {
			log.Warn(`The default logger writes to STDERR. Writing errors there was explicitly requested, but -q or --log were not specified. Writing entire log to STDOUT to avoid double-writing error messages.`)
			log.SetOutput(os.Stdout)
		}
		log.AddHook(metha.NewCopyHook(os.Stderr))
	}
	headers := o.extraHeaders
	if o.basicAuthCreds != "" {
		parts := strings.Split(o.basicAuthCreds, ":")
		if len(parts) != 2 {
			return errors.New("invalid format, we require username:password")
		}
		headers = append(headers, "Authorization: Basic "+basicAuth(parts[0], parts[1]))
	}
	var extra = make(http.Header)
	for _, s := range headers {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf(`extra headers notation is "Some-Key: Some-Value", got %v`, parts)
		}
		extra.Set(parts[0], parts[1])
	}
	harvest, err := metha.NewHarvest(baseURL)
	if err != nil {
		return err
	}
	// if the harvest resulted in any extra header set, add them here
	if harvest.Config.ExtraHeaders != nil {
		for k, vs := range harvest.Config.ExtraHeaders {
			for _, v := range vs {
				extra.Add(k, v)
			}
		}
	}
	if rateLimitBytesPerSec > 0 {
		harvest.Client = metha.CreateClientWithRateLimit(o.timeout, o.maxRetries, rateLimitBytesPerSec)
	} else {
		harvest.Client = metha.CreateClient(o.timeout, o.maxRetries)
	}
	harvest.Config.From = o.from
	harvest.Config.Until = o.until
	harvest.Config.Format = o.format
	harvest.Config.Set = o.set
	harvest.Config.MaxRequests = o.maxRequests
	harvest.Config.CleanBeforeDecode = true
	harvest.Config.DisableSelectiveHarvesting = o.disableSelectiveHarvesting
	harvest.Config.MaxEmptyResponses = o.maxEmptyResponses
	harvest.Config.IgnoreHTTPErrors = o.ignoreHTTPErrors
	harvest.Config.SuppressFormatParameter = o.suppressFormatParameter
	harvest.Config.HourlyInterval = o.hourly
	harvest.Config.DailyInterval = o.daily
	harvest.Config.ExtraHeaders = extra
	harvest.Config.Delay = o.delay
	harvest.Config.KeepTemporaryFiles = o.keepTemporaryFiles
	harvest.Config.IgnoreUnexpectedEOF = o.ignoreUnexpectedEOF
	harvest.Config.NoCompression = o.noCompression
	resolved := o.harvestLayout(identity)
	if o.removeCached {
		log.Printf("removing already cached data for %s", identity.BaseURL)
		if err := store.Remove(o.baseDir, identity, resolved); err != nil {
			log.Println(err)
		}
	}
	if err := o.runHarvest(harvest, identity, resolved); err != nil {
		switch {
		case errors.Is(err, metha.ErrAlreadySynced):
			log.Println("this repository is up-to-date")
			return nil
		case errors.Is(err, metha.ErrLocked):
			// Expected when the same endpoint is handed to two workers,
			// e.g. by the shuf | parallel loop in the README. Not a failure.
			log.Printf("another harvest holds this endpoint, skipping: %v", err)
			return nil
		}
		return err
	}
	return nil
}

// harvestLayout decides where a harvest writes. An endpoint that already has a
// v2 shard keeps it, so a converted cache does not silently start writing files
// beside it again; otherwise --layout, then METHA_LAYOUT, then v1, which is what
// every existing installation has.
func (o *syncOpts) harvestLayout(id store.Identity) store.Layout {
	if o.layout != "" {
		return store.Layout(o.layout)
	}
	if env := os.Getenv(store.LayoutEnv); env != "" {
		return store.Layout(env)
	}
	return store.Detect(o.baseDir, id)
}

// runHarvest points a harvest at the layout it should write into, and runs it.
//
// Every path out of here closes the sink, which is why it is a function of its
// own: an index left open keeps its write-ahead log and shared-memory files on
// disk, with the last committed window still in the log rather than in the
// database.
func (o *syncOpts) runHarvest(harvest *metha.Harvest, id store.Identity, layout store.Layout) error {
	switch layout {
	case store.V1:
		o.noticeOnce(id)
	case store.V2:
		w, err := store.OpenWriter(o.baseDir, id)
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

// noticeName marks a cache whose owner has been told about v2 already.
const noticeName = ".metha-v2-notice"

// noticeOnce tells the user once per cache that their v1 data can be
// converted, with the command that does it, and never mentions it again.
func (o *syncOpts) noticeOnce(id store.Identity) {
	marker := filepath.Join(o.baseDir, noticeName)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	src, err := store.OpenLayout(o.baseDir, id, store.V1)
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

    %s migrate --base-dir %s        # convert everything, keeping the originals
    %s migrate --base-dir %s --rm   # and drop the originals once verified

Harvests continue to work unchanged in v1. This notice is shown once.

`, rootName, o.baseDir, rootName, o.baseDir)
	os.WriteFile(marker, nil, 0644)
}

// parseRateLimit converts a human-readable rate limit string to bytes per second.
func parseRateLimit(input string) (float64, error) {
	if input == "" || input == "0" {
		return 0, nil
	}
	// Remove '/s' suffix if present (e.g., "1MB/s" -> "1MB")
	input = strings.TrimSuffix(strings.ToUpper(input), "/S")
	multiplier := 1.0
	var numStr string
	switch {
	case strings.HasSuffix(input, "KB"):
		multiplier, numStr = 1024, strings.TrimSuffix(input, "KB")
	case strings.HasSuffix(input, "MB"):
		multiplier, numStr = 1024*1024, strings.TrimSuffix(input, "MB")
	case strings.HasSuffix(input, "GB"):
		multiplier, numStr = 1024*1024*1024, strings.TrimSuffix(input, "GB")
	case strings.HasSuffix(input, "K"):
		multiplier, numStr = 1024, strings.TrimSuffix(input, "K")
	case strings.HasSuffix(input, "M"):
		multiplier, numStr = 1024*1024, strings.TrimSuffix(input, "M")
	case strings.HasSuffix(input, "G"):
		multiplier, numStr = 1024*1024*1024, strings.TrimSuffix(input, "G")
	default:
		numStr = input // No unit, assume bytes.
	}
	rate, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid rate limit format: %s", input)
	}
	return rate * multiplier, nil
}

func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
