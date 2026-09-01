package sweep

import (
	"context"
	"time"

	"github.com/miku/metha/harvest"
	"github.com/miku/metha/oai"
	"github.com/miku/metha/store"
)

// Harvester turns the sweep's one Attempt into an actual harvest. It is the
// only place in this package that touches the network or the cache, and it is
// deliberately thin: everything it does, "metha sync" already did, and the
// point is that a swept endpoint and a hand-harvested one leave the same shard.
type Harvester struct {
	BaseDir string
	Format  string
	Set     string

	// Timeout is the HTTP client's, per request, as -T is for sync. It is not
	// the per-endpoint deadline: that belongs to the runner, because it bounds
	// the whole attempt rather than any one request in it.
	Timeout time.Duration
	// Retries bounds both retry layers: the client's and the harvest's own.
	// Kept low for a sweep - an endpoint that needs ten attempts today will be
	// asked again tomorrow, and the budget is better spent on the corpus than
	// on one host. Zero means DefaultRetries; NoRetries means none.
	Retries int
	// MaxBodyBytes bounds one response. Zero means oai.DefaultMaxBodyBytes.
	//
	// A sweep is the reason there is a bound at all: it meets every endpoint
	// there is, so it meets the ones that answer with something no repository
	// would send, and it meets them --jobs at a time.
	MaxBodyBytes int
	// Delay sleeps between requests to one endpoint, on top of the politeness
	// the runner's host partitioning already provides.
	Delay time.Duration
}

// DefaultTimeout and DefaultRetries are what a sweep asks with. Both are lower
// than sync's defaults, and for the same reason: sync is one endpoint an
// operator is waiting on, and a sweep is a quarter of a million it is not. A
// sweep that spends ten retries on a wedged host spends them instead of
// harvesting something else, and gets another turn tomorrow regardless.
const (
	DefaultTimeout = 30 * time.Second
	// DefaultRetries is one, not three, because the two retry layers multiply:
	// three each is nine attempts at a host that is not there, and at thirty
	// seconds apiece that is four and a half minutes of a worker spent proving
	// something the first attempt already showed. One each is four, and the
	// endpoint gets another four tomorrow.
	//
	// Measured over 300 real endpoints from the list: three retries swept 17 of
	// them in eight minutes with sixteen workers. The dead are the whole cost of
	// a sweep, and they are most of what a first sweep meets.
	DefaultRetries = 1
	// NoRetries asks for a single attempt. It is spelled rather than left to a
	// zero because zero is the default here, as it is for every other field,
	// and "one attempt only" is a thing worth being able to say out loud.
	NoRetries = -1
)

// Attempt harvests one endpoint, and is the function a Runner is given.
//
// Its contract is the runner's: it must return when ctx is done. Everything
// under it honours that - the client's request, both stacked retry backoffs,
// and the harvest's own loop between windows - which is what makes the
// per-endpoint deadline a real bound rather than a hope.
func (h *Harvester) Attempt(ctx context.Context, endpoint string) Result {
	id := store.Identity{
		BaseURL: oai.PrependSchema(endpoint),
		Format:  h.format(),
		Set:     h.Set,
	}
	// What the cache held before, so that what it gained is a fact rather than
	// an inference from whether an error came back. An endpoint the cache holds
	// nothing for has not been harvested, which is a count of zero and not a
	// failure to read it.
	before := h.records(id)

	res := Result{Total: before}
	// With the client, not after it. Identify is where a dead endpoint fails,
	// so it is the request whose retry budget decides what the dead cost - and
	// the dead are most of what a sweep spends its time on.
	hv, err := harvest.NewHarvestWithClient(ctx, id.BaseURL, oai.CreateClient(h.timeout(), h.retries()))
	if err != nil {
		res.Err = err
		return res
	}
	res.Quirks = quirksOf(hv)

	hv.Config.Format = id.Format
	hv.Config.Set = id.Set
	hv.Config.Delay = h.Delay
	// As sync sets it: endpoints in the long tail send control characters the
	// XML decoder will not take, and a cache of responses is worth more than a
	// strict parse.
	hv.Config.CleanBeforeDecode = true
	// A dead window skips rather than ending the harvest. For a sweep this is
	// the only sensible setting: one bad month in 2011 must not cost the rest
	// of the endpoint, and the range simply stays uncovered for next time.
	hv.Config.IgnoreHTTPErrors = true
	hv.Config.MaxEmptyResponses = 10
	hv.Config.MaxRequests = 1 << 20
	hv.Config.MaxBodyBytes = h.MaxBodyBytes
	// The harvest's own retry layer sits above the client's, and its defaults -
	// three retries from ten seconds, doubling - are seventy seconds of waiting
	// per window before anything gives up. A sweep wants both layers small: an
	// endpoint that needs a minute of backoff today gets another turn tomorrow,
	// and the minute is better spent on the rest of the corpus.
	hv.Config.MaxRetries = h.retries()
	hv.Config.RetryDelay = 2 * time.Second
	hv.Config.RetryBackoff = 2.0
	// Whatever identify had to do to get an answer, the rest of the harvest
	// does too: it set the headers on this same config, so there is nothing to
	// carry across.

	res.Err = h.run(ctx, hv, id)
	res.Total = h.records(id)
	res.Gained = max(res.Total-before, 0)
	return res
}

// run opens the shard, harvests into it and closes it.
//
// Separate from Attempt only so that every path out of it closes the writer:
// the writer holds the group's lock for its lifetime, and a sweep that leaked
// one would lock an endpoint out of every future sweep until the process died.
func (h *Harvester) run(ctx context.Context, hv *harvest.Harvest, id store.Identity) (err error) {
	w, werr := store.OpenWriter(h.BaseDir, id)
	if werr != nil {
		return werr
	}
	defer func() {
		// Worth reporting only if nothing worse happened: everything committed
		// is already durable, and what Close does is release the lock and drop
		// a window that never reached a commit.
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if err := w.SetIdentify(hv.Identify); err != nil {
		return err
	}
	hv.Writer = w
	return hv.Run(ctx)
}

// records is what the cache holds for an identity, or zero.
//
// The cache is authoritative for what was harvested and the roster only for
// what was attempted, so this is the direction the two are reconciled in: an
// endpoint someone harvested by hand shows up here, and the roster catches up
// at the next sweep. An identity the cache holds nothing for is not an error -
// it is every endpoint's first sweep - and a shard that cannot be read at all
// reports zero rather than failing the attempt, since the count is only ever
// used to tell "gained something" from "gained nothing".
func (h *Harvester) records(id store.Identity) int {
	st, err := store.Stat(h.BaseDir, id)
	if err != nil || st == nil {
		return 0
	}
	return st.Records
}

// quirksOf reads what a harvest already learned about how its endpoint has to
// be asked. Passive only: nothing here costs a request, which is what makes it
// affordable for every endpoint on every sweep.
//
// The Accept-Encoding one is the quirk that pays for itself, and it is readable
// without harvest reporting anything new: identify sets that header on the
// config it shares, and only on its workaround path, so the header's presence
// afterwards is the fingerprint of having needed it. A later sweep can then ask
// for identity encoding up front instead of spending a failed request
// rediscovering the same thing.
func quirksOf(hv *harvest.Harvest) *Quirks {
	if hv == nil || hv.Identify == nil {
		return nil
	}
	q := Quirks{
		Granularity:   hv.Identify.Granularity,
		DeletedRecord: hv.Identify.DeletedRecord,
	}
	if hv.Config != nil && hv.Config.ExtraHeaders.Get("Accept-Encoding") == "identity" {
		q.IdentityEncoding = true
	}
	if q == (Quirks{}) {
		return nil
	}
	return &q
}

func (h *Harvester) format() string {
	if h.Format == "" {
		return "oai_dc"
	}
	return h.Format
}

func (h *Harvester) timeout() time.Duration {
	if h.Timeout <= 0 {
		return DefaultTimeout
	}
	return h.Timeout
}

func (h *Harvester) retries() int {
	if h.Retries < 0 {
		return 0
	}
	if h.Retries == 0 {
		return DefaultRetries
	}
	return h.Retries
}
