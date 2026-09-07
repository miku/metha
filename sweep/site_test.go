package sweep

import "testing"

// TestSiteFoldsWhatIsOneServer is the politeness key's contract. Every case
// here is a shape the 2026-09-06 roster actually holds, and the first two
// blocks are the ones a hostname key got wrong at a cost of roughly a third of
// the corpus's transient class.
func TestSiteFoldsWhatIsOneServer(t *testing.T) {
	for _, tt := range []struct {
		name string
		urls []string
		want string
	}{{
		// 530 WEKO repositories, one NII server, and NII limits concurrency
		// rather than rate. Under a hostname key these were 530 politeness
		// keys and the sweep ran 64 of them at once for 1,151 transients.
		name: "NII subdomains are one server",
		urls: []string{
			"https://seisa.repo.nii.ac.jp/oai",
			"https://reitaku.repo.nii.ac.jp/oai",
			"http://LibKGC.repo.nii.ac.jp/oai",
		},
		want: "nii.ac.jp",
	}, {
		// www.ajol.info holds 664 endpoints and ajol.info another 6. Two keys,
		// both dispatched at once by construction, 661 identical EOFs.
		name: "www and bare are one server",
		urls: []string{
			"https://www.ajol.info/index.php/ajol/oai",
			"https://ajol.info/index.php/ajol/oai",
			"http://AJOL.info/index.php/x/oai",
		},
		want: "ajol.info",
	}, {
		name: "scheme and port are not part of the key",
		urls: []string{
			"http://raco.cat/index.php/index/oai",
			"https://raco.cat:8443/index.php/index/oai",
			"https://www.raco.cat/index.php/index/oai",
		},
		want: "raco.cat",
	}, {
		// Multi-label public suffixes are why this needs the list rather than
		// a label count: .ac.id, .ac.uk, .com.br and .edu.my all appear in the
		// corpus, and counting labels either merges every UK university into
		// one key or splits NII back into 530.
		name: "ac.id is a public suffix",
		urls: []string{"http://ejournal.unsrat.ac.id/index.php/jkkt/oai"},
		want: "unsrat.ac.id",
	}, {
		name: "ac.uk is a public suffix",
		urls: []string{"https://eprints.gla.ac.uk/cgi/oai2"},
		want: "gla.ac.uk",
	}, {
		name: "com.br is a public suffix",
		urls: []string{"https://ojs.example.com.br/index.php/a/oai"},
		want: "example.com.br",
	}, {
		// A raw IP has no registrable domain and needs none: it is already
		// exactly one server. The corpus holds 152 endpoints on this one.
		name: "a raw IP is its own key",
		urls: []string{"http://146.164.170.163/ojs/index.php/a/oai"},
		want: "146.164.170.163",
	}, {
		name: "an unparseable URL is its own key",
		urls: []string{"http/not a url"},
		want: "http/not a url",
	}} {
		t.Run(tt.name, func(t *testing.T) {
			for _, u := range tt.urls {
				if got := Site(u); got != tt.want {
					t.Errorf("Site(%q) = %q, want %q", u, got, tt.want)
				}
			}
		})
	}
}

// TestSiteKeepsUnrelatedOperatorsApart is the other half of the contract, and
// the reason the key is eTLD+1 rather than something coarser. Two universities
// under .ac.uk are two operators; merging them would serialise the entire
// British corpus behind whichever one is slowest.
func TestSiteKeepsUnrelatedOperatorsApart(t *testing.T) {
	for _, pair := range [][2]string{
		{"https://eprints.gla.ac.uk/cgi/oai2", "https://ora.ox.ac.uk/oai"},
		{"https://a.example.com.br/oai", "https://b.example2.com.br/oai"},
		{"http://146.164.170.163/oai", "http://146.164.170.164/oai"},
		{"https://x.blogspot.com/oai", "https://y.blogspot.com/oai"},
	} {
		if Site(pair[0]) == Site(pair[1]) {
			t.Errorf("Site(%q) == Site(%q) == %q; want different keys",
				pair[0], pair[1], Site(pair[0]))
		}
	}
}
