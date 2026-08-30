package harvest

import (
	"time"

	dates "github.com/jinzhu/now"

	"github.com/miku/metha/oai"
)

// Segmentation is how the settled past is cut into windows. One window is one
// pass over an endpoint's resumption tokens, so the choice trades the number of
// requests against how much work a single failure throws away.
type Segmentation int

const (
	// Monthly is what an endpoint that behaves is harvested with.
	Monthly Segmentation = iota
	// Daily and Hourly are for endpoints that cannot carry a month's worth of
	// records through a token chain.
	Daily
	Hourly
)

// period is the calendar unit this segmentation cuts along.
func (s Segmentation) period() period {
	switch s {
	case Hourly:
		return byHour
	case Daily:
		return byDay
	default:
		return byMonth
	}
}

// Window is one range a harvest fetches in a single pass over an endpoint's
// resumption tokens, together with what may be believed about the result.
type Window struct {
	Interval

	// Settled reports whether the window ends before the point at which the
	// endpoint's datestamps can still change. What an unsettled window holds is
	// only what existed at the moment of asking, so the next run fetches the
	// same range again rather than resuming past it.
	Settled bool
}

// Boundless reports whether the window claims no range at all. That is the
// single window of a harvest that cannot ask for one - see PlanConfig.Unbounded
// - and it stands for the whole repository as of now. Making the same claim on
// every run is the point: a re-harvest replaces the window it already has
// instead of stacking another copy of the endpoint beside it.
func (w Window) Boundless() bool { return w.Begin.IsZero() && w.End.IsZero() }

// String formats the window, saying so when it is not final.
func (w Window) String() string {
	switch {
	case w.Boundless():
		return "[whole repository]"
	case w.Settled:
		return w.Interval.String()
	}
	return w.Interval.String() + " (partial)"
}

// Coverage is what a store already holds, in the terms a plan is made in. It is
// a value, not a database: whoever holds the data answers this once, and the
// plan is then a pure function of it.
type Coverage struct {
	// Resume is where the next harvest picks up, or the zero time if this group
	// was never harvested. A store that can tell a settled window from one that
	// only holds what existed at the moment of asking points back at the start
	// of the latter, so that range is covered again rather than resumed past.
	Resume time.Time
}

// PlanConfig is the part of a harvest's configuration that decides what to
// fetch, as opposed to how to ask for it or where to put it.
type PlanConfig struct {
	// From and Until bound the harvest, in the 2006-01-02 form the flags take.
	// From replaces the endpoint's earliest datestamp as the start of the data;
	// Until replaces the reachable end. Either may be empty.
	From, Until string

	// Segmentation cuts the settled past into windows.
	Segmentation Segmentation

	// Unbounded harvests the endpoint in one go, for endpoints that cannot
	// answer a from and until at all (--no-intervals). The plan is then a single
	// boundless window; nothing else here applies.
	Unbounded bool
}

// Plan decides what a harvest fetches, and is the only place that decides it.
//
// It is a pure function of what the store already holds, what the endpoint says
// about itself, the clock and the configuration - no network, no disk. Every
// date rule metha has is a step in here: where the data starts, where a harvest
// resumes, how far into the present a request can reach, where the still
// changing present begins, and how the settled past is cut up. Each of them is
// therefore a row in a table test rather than a network harvest and a database.
func Plan(cov Coverage, id *oai.Identify, now time.Time, cfg PlanConfig) ([]Window, error) {
	if cfg.Unbounded {
		// No range was requested, so the window claims none, and it is never
		// settled: a fetch that could not say what it asked for cannot claim to
		// have covered anything.
		return []Window{{}}, nil
	}
	iv, err := plannedInterval(cov, id, now, cfg)
	if err != nil {
		return nil, err
	}
	// The settled part is cut into windows. What is left reaches into the
	// endpoint's still-changing present and stays one window, so that the next
	// run repeats exactly that range instead of a growing tail of it: tomorrow
	// the same split leaves today's window behind as a settled one, whose range
	// matches to the nanosecond and so replaces it.
	settled, unsettled := iv.SplitAt(settledFrom(id, now))
	var windows []Window
	for _, part := range settled.cut(cfg.Segmentation.period()) {
		windows = append(windows, Window{Interval: part, Settled: true})
	}
	if !unsettled.Empty() {
		windows = append(windows, Window{Interval: unsettled})
	}
	return windows, nil
}

// plannedInterval is the whole span a run covers, before it is cut up: from
// wherever the last run left off, or the start of the data if there was none,
// to as far as this run may ask.
func plannedInterval(cov Coverage, id *oai.Identify, now time.Time, cfg PlanConfig) (Interval, error) {
	// Asked for even when a resume point makes it moot, so that an endpoint
	// whose granularity cannot be read is refused here rather than harvested
	// with silently unbounded requests - formatBound drops the bounds from every
	// request in that case.
	begin, err := dataStart(id, cfg.From)
	if err != nil {
		return Interval{}, err
	}
	if !cov.Resume.IsZero() {
		begin = cov.Resume
	}
	end, err := plannedEnd(id, now, cfg.Until)
	if err != nil {
		return Interval{}, err
	}
	if begin.After(end) {
		return Interval{}, ErrAlreadySynced
	}
	return Interval{Begin: begin, End: end}, nil
}

// dataStart is where an endpoint's data begins: what it says itself, or what the
// caller said instead.
//
// A date given as a date is read in the local zone, the one the window
// boundaries are computed in, so that the two can be compared. refs #9100
func dataStart(id *oai.Identify, from string) (time.Time, error) {
	if from != "" {
		return time.ParseInLocation("2006-01-02", from, time.Local)
	}
	return id.EarliestDate()
}

// plannedEnd is the right edge of the span a run covers.
func plannedEnd(id *oai.Identify, now time.Time, bound string) (time.Time, error) {
	if bound == "" {
		return reachableEnd(id, now), nil
	}
	until, err := time.ParseInLocation("2006-01-02", bound, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	// A date-only bound means the whole of that day, which is how the endpoint
	// reads it; spelling it out keeps the window that gets recorded honest, and
	// stops a second granularity request from asking for midnight and so missing
	// the day it was given.
	return dates.New(until).EndOfDay(), nil
}

// reachableEnd returns how far into the present a harvest can ask, which is as
// far as the endpoint's granularity lets a request reach. An endpoint that
// speaks only dates cannot be asked for less than a whole day, so the harvest
// takes the whole of today and records it as unsettled; see settledFrom.
func reachableEnd(id *oai.Identify, now time.Time) time.Time {
	if id.SecondGranularity() {
		return now.Truncate(time.Second)
	}
	return dates.New(now).EndOfDay()
}

// settledFrom returns the instant from which an endpoint's datestamps can still
// change, and so the point a harvest must come back to on its next run.
//
// The problem it answers: a window is remembered by the range it covered, and
// with daily granularity "until today" is the only thing a request can say. Ask
// it at noon and the answer holds the morning's records only - but the window
// claims the whole day, so a run tomorrow would resume past it and lose the
// afternoon for good. Anything at or after this point is therefore recorded as
// unsettled and fetched again.
//
// Truncated to the second, the finest an OAI request can express, which also
// keeps stored boundaries comparable as strings.
func settledFrom(id *oai.Identify, now time.Time) time.Time {
	if id.SecondGranularity() {
		// Datestamps are exact here, so only the recent past is in doubt: a
		// clock that runs behind ours, or a record indexed a moment after it was
		// stamped, would otherwise land just before a boundary we have already
		// passed.
		//
		// Truncated to whole lags rather than to the second, so that the
		// boundary stands still between runs the way BeginningOfDay does below.
		// A boundary that moved with the clock made every re-run split off a
		// settled window a few seconds wide - one request and one row each time,
		// for a sliver of time nothing happened in. Quantised, a re-run inside
		// the same lag asks the one question that is still open, and the window
		// it commits replaces the one before it.
		return now.Add(-SettleLag).Truncate(SettleLag)
	}
	return dates.New(now).BeginningOfDay()
}
