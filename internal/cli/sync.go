package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/miku/metha"
	"github.com/miku/metha/harvest"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// syncOpts holds what the sync flags are parsed into. It is a struct rather
// than a run of closure variables only because there are two dozen of them, and
// the harvest wants most of them together.
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
	ignoreUnexpectedEOF        bool
	rateLimit                  string
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
				for _, u := range metha.Endpoints() {
					fmt.Println(u)
				}
				os.Exit(0)
			}
			if len(args) == 0 {
				return fmt.Errorf("an endpoint URL is required, maybe try: %s", metha.RandomEndpoint())
			}
			return o.run(cmd.Context(), args[0])
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.baseDir, "base-dir", metha.GetBaseDir(), "base dir for harvested files")
	f.BoolVar(&o.hourly, "hourly", false, "use hourly intervals for harvesting")
	f.BoolVar(&o.daily, "daily", false, "use daily intervals for harvesting")
	f.DurationVar(&o.delay, "delay", 0, "sleep between each OAI-PMH request")
	f.BoolVar(&o.disableSelectiveHarvesting, "no-intervals", false, "harvest in one go, for funny endpoints; refetches everything each run, so use -rm to reclaim the space")
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
	f.BoolVar(&o.ignoreUnexpectedEOF, "ignore-unexpected-eof", false, "ignore unexpected EOF")
	f.StringVar(&o.rateLimit, "rate-limit", "", "download rate limit (e.g., '1MB', '500KB', '2.5MB/s', '1024'); if no unit specified, bytes/sec assumed; set to 0 or empty to disable")
	f.StringArrayVarP(&o.extraHeaders, "header", "H", nil, `extra HTTP header to pass to requests (repeatable); e.g. -H "token: 123"`)
	return cmd
}

func (o *syncOpts) run(ctx context.Context, endpoint string) error {
	rateLimitBytesPerSec, err := parseRateLimit(o.rateLimit)
	if err != nil {
		return fmt.Errorf("invalid rate limit: %w", err)
	}
	if rateLimitBytesPerSec > 0 {
		log.Printf("Rate limiting enabled: %.2f bytes/sec (%.2f KB/s)",
			rateLimitBytesPerSec, rateLimitBytesPerSec/1024)
	}
	baseURL := oai.PrependSchema(endpoint)
	identity := store.Identity{BaseURL: baseURL, Format: o.format, Set: o.set}
	// Before anything else, and before the network: an endpoint still in the
	// pre-1.0 layout has data this harvest cannot see, and harvesting it again
	// into a shard beside it would leave two half caches.
	if err := store.CheckLegacy(o.baseDir, identity); err != nil {
		return err
	}
	if o.showDir {
		st, err := store.Open(o.baseDir, identity)
		if err != nil {
			return err
		}
		fmt.Println(st.Dir())
		os.Exit(0)
	}
	if o.quiet {
		log.SetOutput(io.Discard)
		routeStdlibLog(io.Discard)
	}
	if o.logFile != "" {
		file, err := os.OpenFile(o.logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("error opening log file: %w", err)
		}
		log.SetOutput(file)
		routeStdlibLog(file)
	}
	if o.logStderr {
		if !o.quiet && o.logFile == "" {
			log.Warn(`The default logger writes to STDERR. Writing errors there was explicitly requested, but -q or --log were not specified. Writing entire log to STDOUT to avoid double-writing error messages.`)
			log.SetOutput(os.Stdout)
		}
		log.AddHook(NewCopyHook(os.Stderr))
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
	// The client is built before the harvest, not after it. Identify is the
	// first request and the one a dead URL fails on, so it is the request that
	// decides what a bad endpoint costs - and until this was passed in, it was
	// made on the default client, ignoring -T and -r entirely. See
	// harvest.NewHarvestWithClient.
	var client *oai.Client
	if rateLimitBytesPerSec > 0 {
		client = oai.CreateClientWithRateLimit(o.timeout, o.maxRetries, rateLimitBytesPerSec)
	} else {
		client = oai.CreateClient(o.timeout, o.maxRetries)
	}
	h, err := harvest.NewHarvestWithClient(ctx, baseURL, client)
	if err != nil {
		if errors.Is(err, harvest.ErrNotAnEndpoint) {
			// Almost always a host where a path was meant. Worth saying,
			// because the reply to a wrong URL is a perfectly good web page and
			// gives no hint that anything is off.
			return fmt.Errorf("%w\nan OAI-PMH base URL is usually a path rather than a host, as in %s/oai", err, strings.TrimRight(baseURL, "/"))
		}
		return err
	}
	// if the harvest resulted in any extra header set, add them here
	if h.Config.ExtraHeaders != nil {
		for k, vs := range h.Config.ExtraHeaders {
			for _, v := range vs {
				extra.Add(k, v)
			}
		}
	}
	h.Config.From = o.from
	h.Config.Until = o.until
	h.Config.Format = o.format
	h.Config.Set = o.set
	h.Config.MaxRequests = o.maxRequests
	h.Config.CleanBeforeDecode = true
	h.Config.DisableSelectiveHarvesting = o.disableSelectiveHarvesting
	h.Config.MaxEmptyResponses = o.maxEmptyResponses
	h.Config.IgnoreHTTPErrors = o.ignoreHTTPErrors
	h.Config.SuppressFormatParameter = o.suppressFormatParameter
	h.Config.HourlyInterval = o.hourly
	h.Config.DailyInterval = o.daily
	h.Config.ExtraHeaders = extra
	h.Config.Delay = o.delay
	h.Config.IgnoreUnexpectedEOF = o.ignoreUnexpectedEOF
	if o.removeCached {
		log.Printf("removing already cached data for %s", identity.BaseURL)
		if err := store.Remove(o.baseDir, identity); err != nil {
			log.Println(err)
		}
	}
	if err := o.runHarvest(ctx, h, identity); err != nil {
		switch {
		case errors.Is(err, harvest.ErrAlreadySynced):
			log.Println("this repository is up-to-date")
			return nil
		case errors.Is(err, context.Canceled):
			// An interrupt, and it did what it was asked: the window in flight
			// was dropped and everything committed before it stands. The next
			// run resumes from there, so this is not a failure to report.
			log.Println("interrupted; the windows committed so far are kept")
			return nil
		case errors.Is(err, store.ErrLocked):
			// Expected when the same endpoint is handed to two workers,
			// e.g. by the shuf | parallel loop in the README. Not a failure.
			// The lock is the group's, so this is the same format and set
			// already being harvested; another format of the same endpoint
			// runs alongside.
			log.Printf("another harvest holds this format of this endpoint, skipping: %v", err)
			return nil
		}
		return err
	}
	return nil
}

// runHarvest opens the shard a harvest writes into, and runs it.
//
// Every path out of here closes the writer, which is why it is a function of
// its own: the writer holds the group's lock for its lifetime, and an interrupt
// now arrives as a cancelled context rather than as a signal handler racing the
// commit, so the deferred Close is what releases it.
func (o *syncOpts) runHarvest(ctx context.Context, h *harvest.Harvest, id store.Identity) error {
	w, err := store.OpenWriter(o.baseDir, id)
	if err != nil {
		return err
	}
	// Worth saying if it fails, not worth failing the harvest over: everything
	// committed is already durable, and what Close does is release the lock and
	// drop a window that never reached a commit.
	defer func() {
		if err := w.Close(); err != nil {
			log.Printf("closing the shard: %v", err)
		}
	}()
	// The identify response is what makes a shard self-describing: granularity
	// and earliest datestamp are the two things a harvest needs, and the
	// pre-1.0 layout had nowhere to keep them.
	if err := w.SetIdentify(h.Identify); err != nil {
		return err
	}
	h.Writer = w
	log.Printf("harvest: %+v", h)
	if err := h.Run(ctx); err != nil {
		return err
	}
	if o.disableSelectiveHarvesting {
		warnUnbounded(w)
	}
	return nil
}

// unboundedWarnBytes is the size at which a -no-intervals cache is worth
// mentioning. It is large enough that an endpoint harvested once or twice never
// trips it, and small enough to arrive well before a disk fills.
const unboundedWarnBytes = 10 << 30

// warnUnbounded says what -no-intervals costs once it has cost enough to
// notice. An endpoint that cannot answer a from and until has to be fetched
// whole every time, so each run appends another copy of it. Reads show only the
// newest, so the harvest stays correct; the disk does not.
//
// The older copies are not dropped here. Whether they are worth their bytes
// depends on something metha cannot see: an endpoint that has gone away, or
// that has quietly dropped records, leaves them as the only surviving copy.
// Discarding them is a decision, and -rm is where the user makes it.
func warnUnbounded(w *store.Writer) {
	n := w.SegmentBytes()
	if n < unboundedWarnBytes {
		return
	}
	log.Warnf("-no-intervals stores the whole endpoint again on every run, and %s holds %.1f GB of them by now; reads return only the newest, the rest is dead weight. Harvest with -rm to start from one copy again.",
		w.Dir(), float64(n)/(1<<30))
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
