package sweep

import (
	"time"

	"github.com/miku/metha/store"
)

// Reconcile adds endpoints the cache holds that the roster does not, and
// reports how many it added.
//
// The direction is the whole point, and it is the answer to the "two sources of
// truth" risk: the cache is authoritative for *what was harvested* and the
// roster only for *what was attempted*, so when they disagree the roster is
// what gets corrected. A user is free to harvest any endpoint by hand, restore
// a cache from backup, or copy shards between machines; the roster catches up
// at the next sweep rather than having to be told.
//
// It runs at the start of every sweep rather than behind a flag. Walking the
// cache costs about eighty seconds warm against 244k shards, which is a tenth
// of a percent of a daily budget - too cheap to be worth an option, and an
// option is only ever an opportunity to have it switched off on the machine
// where it mattered.
//
// Only entries of this roster's format and set are considered. A shard of some
// other format is not this sweep's business, and adding it would put an
// endpoint in the roster that the sweep would then harvest as oai_dc, which is
// not what its presence in the cache is evidence of.
func (r *Roster) Reconcile(baseDir string) (int, error) {
	format, set := r.Header().Format, r.Header().Set
	var added int
	for entry, err := range store.List(baseDir) {
		if err != nil {
			return added, err
		}
		id := entry.Identity
		if id.Format != format || id.Set != set || id.BaseURL == "" {
			continue
		}
		if _, ok := r.Get(id.BaseURL); ok {
			continue
		}
		p := Profile{
			URL:       id.BaseURL,
			State:     StateActive,
			FirstSeen: r.clock(),
		}
		// What the shard says about itself, so that an endpoint harvested by
		// hand an hour ago is not swept again immediately. A shard that cannot
		// say when it was last harvested is simply due now, which is the
		// conservative answer: at worst it costs one request.
		if st, err := store.Stat(baseDir, id); err == nil && st != nil {
			p.Records = st.Records
			if !st.LastSeen.IsZero() {
				p.LastOK = st.LastSeen.UTC()
				p.LastAttempt = p.LastOK
				p.LastClass = ClassOK
				p.NextDue = Due(p, p.LastOK, DefaultPolicy())
			}
		}
		if err := r.Put(p); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// clock is the roster's notion of now, so that Reconcile and Seed agree with
// each other and with the tests.
func (r *Roster) clock() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Now == nil {
		return time.Now().UTC()
	}
	return r.Now().UTC()
}
