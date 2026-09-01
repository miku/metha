package sweep

import (
	"net/url"
	"strings"

	"github.com/miku/metha/oai"
)

// Seeds turns the lines of an endpoint list into URLs a sweep can attempt.
//
// The list is a hypothesis rather than an input - a third of contrib/sites.tsv
// has no "oai" anywhere in it and a good many entries are not endpoints at all -
// so this cleans what it can and drops only what it cannot read. What it must
// not do is guess: an entry that is merely unlikely stays, because the roster
// is where that gets settled empirically, at the cost of one request every
// ninety days.
//
// 778 of the 244,346 lines carry whitespace, in three shapes that want three
// different answers:
//
//	http:// http://je-lks.maieutiche.economia.unitn.it/index.php/Je-LKS/oai
//	https://…/jmpisearch?authors=Muhammad Rizki
//	http://ainfo.…/1796-5233-1-PB1.pdf; http://www.revistas2.uepg.br/…/oai
//
// The first is a stray prefix with the real URL after it, so the junk has to
// go; the second is one URL with a space inside it, so the head is the part
// worth keeping; the third is a citation link followed by the real endpoint,
// where the junk is the *longer* of the two. No positional rule gets all three,
// which is why this looks at what the fields are rather than where they sit:
// require a host that could exist, prefer a field that says "oai", and fall
// back to the longest.
//
// This line is also what the old shell pipeline tripped over. "parallel -I {}"
// substituted the raw line, so the binary was handed two arguments, took
// args[0], and harvested the literal string "http://" - which is where the
// 249-second retry loop was being spent, on every pass, forever.
func Seeds(lines []string) []string {
	out := make([]string, 0, len(lines))
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		u := seedURL(line)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// seedURL picks the URL out of one line, or returns the empty string when the
// line holds none.
//
// A single-field line - which is 243,568 of them - takes the fast path and is
// returned as it stands, so none of the reasoning below can touch the lines
// that were never in doubt.
func seedURL(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 1 {
		return fieldURL(fields[0])
	}
	var best, bestOAI string
	for _, field := range fields {
		candidate := fieldURL(field)
		if candidate == "" {
			continue
		}
		if len(candidate) > len(best) {
			best = candidate
		}
		// A field that says "oai" is the endpoint, where a field that does not
		// is a citation link that happened to be pasted beside one. This
		// preference applies only within a line that was already malformed:
		// about 35,800 perfectly good endpoints have no "oai" in them, and none
		// of them reach this branch.
		if strings.Contains(strings.ToLower(candidate), "oai") && len(candidate) > len(bestOAI) {
			bestOAI = candidate
		}
	}
	if bestOAI != "" {
		return bestOAI
	}
	return best
}

// fieldURL reads one whitespace-separated field as a URL, recovering it from a
// junk prefix where it can.
//
// The fourth shape in the list has no whitespace in it at all, so nothing above
// separates it:
//
//	http://.http://www.gts.energy-journals.ru/index.php/GTS/oai
//	http://<http://revistas.ucm.es/index.php/ASEM/oai
//
// 214 lines are glued together like this, and the real URL is the tail. Cutting
// to the last scheme would recover them - and would also wreck a perfectly good
// endpoint that carries a URL in a query parameter, "…/oai?url=http://b.test/".
// What separates the two is where the second scheme sits: inside the authority
// it is junk glued to the front of the real URL, and after the authority begins
// it is part of a path or query that the endpoint means.
//
// "Does it parse?" is not enough on its own, because some of these glue cleanly:
// "http://journal.binadarmhttp://journal.binadarma.ac.id/…" has the perfectly
// resolvable-looking host "journal.binadarmhttp".
func fieldURL(field string) string {
	candidate := oai.PrependSchema(strings.Trim(field, "(),;<>\"'"))
	if i := gluedScheme(candidate); i > 0 {
		if tail := candidate[i:]; plausible(tail) {
			return tail
		}
	}
	if plausible(candidate) {
		return candidate
	}
	return ""
}

// gluedScheme returns the offset of a second scheme glued into s's authority,
// or -1 when there is none.
func gluedScheme(s string) int {
	const sep = "://"
	first := strings.Index(s, sep)
	if first < 0 {
		return -1
	}
	// The authority runs from just after the first "://" to whatever starts the
	// path, query or fragment.
	authority := first + len(sep)
	end := len(s)
	if i := strings.IndexAny(s[authority:], "/?#"); i >= 0 {
		end = authority + i
	}
	i := strings.LastIndex(s, sep)
	if i <= first || i >= end {
		return -1
	}
	// Back up over the scheme name in front of it, and only accept one we speak
	// - so that a stray "mailto://" inside a host is left where it is.
	for _, scheme := range []string{"https", "http"} {
		if start := i - len(scheme); start >= 0 && s[start:i] == scheme {
			return start
		}
	}
	return -1
}

// plausible reports whether a string could be an endpoint URL. It asks almost
// nothing - a scheme we can speak, and a host that could exist - because
// anything more would be this function deciding what an endpoint looks like,
// and the list is full of endpoints that do not look like one.
func plausible(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return validHost(u.Hostname())
}

// validHost reports whether a host could be resolved: dot-separated labels,
// none of them empty, nothing in them that a name cannot contain.
//
// Stricter than it looks like it needs to be, and every clause is paid for by
// something in the list. The empty host is the stray "http://" prefix. The
// empty label is ".http" and ".revistas.unifacs.br", where a junk character was
// glued to the front of a real name. And the dot requirement is what stops
// "Rizki" - the tail of a URL split on an internal space - from becoming an
// endpoint the sweep chases for a year.
func validHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if host == "" || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
				r >= '0' && r <= '9', r == '-', r == '_':
			default:
				return false
			}
		}
	}
	return true
}
