package sweep

import (
	"slices"
	"testing"
)

// TestSeedURL is the list-cleaning table. Every malformed row here is taken
// verbatim from contrib/sites.tsv, because the shapes are the point: a rule
// invented against imagined input would get the three real ones wrong, which is
// what happened to the first version of this.
func TestSeedURL(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		// The ordinary case, which is 243,568 of the 244,346 lines.
		{
			"a plain endpoint",
			"http://export.arxiv.org/oai2",
			"http://export.arxiv.org/oai2",
		},
		{
			// A third of the list has no "oai" anywhere in it and is harvested
			// perfectly well.
			"an endpoint that does not say oai",
			"http://repository.example.org/cgi/handler",
			"http://repository.example.org/cgi/handler",
		},
		{
			"a scheme is added when missing",
			"nagano-nct.repo.nii.ac.jp/oai",
			"http://nagano-nct.repo.nii.ac.jp/oai",
		},

		// Shape one: a stray prefix, the real URL after it. This is the line the
		// shell pipeline handed to the binary as two arguments, so that it
		// harvested the literal string "http://" - for 249 seconds, on every
		// pass, forever.
		{
			"a stray http:// prefix",
			"http:// http://je-lks.maieutiche.economia.unitn.it/index.php/Je-LKS/oai",
			"http://je-lks.maieutiche.economia.unitn.it/index.php/Je-LKS/oai",
		},
		{
			"a stray prefix and no scheme on the real one",
			"http:// nagano-nct.repo.nii.ac.jp/oai",
			"http://nagano-nct.repo.nii.ac.jp/oai",
		},

		// Shape two: one URL with a space inside it. The head is what is worth
		// keeping, and the tail is a fragment of a person's name.
		{
			"a search URL split on a space",
			"https://jurnal-pharmaconmw.com/jmpi/index.php/jmpisearch?authors=Muhammad Rizki",
			"https://jurnal-pharmaconmw.com/jmpi/index.php/jmpisearch?authors=Muhammad",
		},

		// Shape three, and the one that rules out every positional rule: a
		// citation link followed by the real endpoint, where the junk is the
		// longer of the two.
		{
			"a citation link before the endpoint",
			"http://ainfo.cnptia.embrapa.br/digital/bitstream/item/25432/1/1796-5233-1-PB1.pdf; http://www.revistas2.uepg.br/index.php/biologica/oai",
			"http://www.revistas2.uepg.br/index.php/biologica/oai",
		},

		// Shape four, which has no whitespace at all, so nothing above
		// separates it: junk glued to the front of the real URL. 214 lines.
		{
			"a dot glued to the front",
			"http://.http://www.gts.energy-journals.ru/index.php/GTS/oai",
			"http://www.gts.energy-journals.ru/index.php/GTS/oai",
		},
		{
			"a bracket glued to the front",
			"http://<http://revistas.ucm.es/index.php/ASEM/oai",
			"http://revistas.ucm.es/index.php/ASEM/oai",
		},
		{
			// The one that shows why "does it parse?" is not enough on its own:
			// the glued host looks perfectly resolvable.
			"a truncated host glued to the real one",
			"http://journal.binadarmhttp://journal.binadarma.ac.id/index.php/BINAMANAJEMEN/oai",
			"http://journal.binadarma.ac.id/index.php/BINAMANAJEMEN/oai",
		},
		{
			"schemes that differ across the glue",
			"http://www.aecpa.eshttps://recyt.fecyt.es/index.php/recp/oai",
			"https://recyt.fecyt.es/index.php/recp/oai",
		},
		{
			// And the case the recovery must not touch: an endpoint that
			// carries a URL in its query, which is a URL the endpoint means.
			"a URL in a query parameter is left alone",
			"http://a.test/oai?url=http://b.test/thing",
			"http://a.test/oai?url=http://b.test/thing",
		},

		// Nothing to read.
		{"empty", "", ""},
		{"whitespace only", "   \t ", ""},
		{"the bare prefix on its own", "http://", ""},
		{"a name fragment on its own", "Rizki", ""},
		{"not a URL at all", "see the website", ""},
		{"a scheme we do not speak", "ftp://files.example.org/oai", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := seedURL(test.line); got != test.want {
				t.Errorf("seedURL(%q) =\n got %q\nwant %q", test.line, got, test.want)
			}
		})
	}
}

// TestSeeds covers what Seeds adds on top of seedURL: order, and the fact that
// the same URL twice is one endpoint.
func TestSeeds(t *testing.T) {
	got := Seeds([]string{
		"http://b.test/oai",
		"",
		"http://a.test/oai",
		"http://b.test/oai", // the same line again
		"http://",           // nothing to read
		"b.test/oai",        // the same URL, spelled without its scheme
	})
	want := []string{"http://b.test/oai", "http://a.test/oai"}
	if !slices.Equal(got, want) {
		t.Errorf("Seeds() = %v, want %v", got, want)
	}
}

// TestSeedsKeepsSchemeDuplicates: http and https of one endpoint stay two
// endpoints. Collapsing them needs the baseURL each one's Identify states,
// which is a request rather than a rule, and guessing here would silently drop
// 613 endpoints on the chance they are duplicates.
func TestSeedsKeepsSchemeDuplicates(t *testing.T) {
	got := Seeds([]string{"http://a.test/oai", "https://a.test/oai"})
	if len(got) != 2 {
		t.Errorf("Seeds() = %v, want both spellings kept", got)
	}
}

func TestPlausible(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"http://a.test/oai", true},
		{"https://a.test/oai", true},
		{"http://localhost:8080/oai", true},
		{"http://127.0.0.1:8080/oai", true},
		{"http://a-b_c.test/oai", true},
		// A host with no dot is the fragment left by a URL split on a space. It
		// parses, it resolves nowhere, and it would be chased for a year.
		{"http://Rizki", false},
		{"http://", false},
		{"ftp://a.test/oai", false},
		{"", false},
		// An empty label: a junk character glued to the front of a real name.
		{"http://.revistas.unifacs.br/oai", false},
		{"http://a..test/oai", false},
		{"http://a.test./oai", false},
	}
	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			if got := plausible(test.in); got != test.want {
				t.Errorf("plausible(%q) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}
