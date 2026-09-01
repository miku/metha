package oai

import (
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
)

// TestDescriptionJSONRoundTrip: the schema allows any number of <description>
// elements and leaves their content open, so a repository is free to put a
// sentence of prose where an oai-identifier block belongs. That marshals to a
// JSON string rather than a JSON object, and a string is not something a struct
// can be unmarshalled from - which failed the whole document, not the field:
//
//	json: cannot unmarshal string into Meta.identify.description.0 of type oai.Description
//
// One such endpoint therefore cost the entire shard, meta.json and all.
func TestDescriptionJSONRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{
			name: "an element, as the schema intends",
			body: `<oai-identifier xmlns="http://www.openarchives.org/OAI/2.0/oai-identifier">` +
				`<scheme>oai</scheme><delimiter>:</delimiter></oai-identifier>`,
		},
		{
			// research-explorer.ista.ac.at, second of two descriptions.
			name: "prose, which is what breaks",
			body: "ISTA Research Explorer (ISTA REx) is the institutional repository " +
				"presenting the scholarly output of the Instutite of Science and Technology Austria.",
		},
		{"empty", ""},
		{"whitespace", "\n\t"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first, err := json.Marshal(Description{Body: []byte(tt.body)})
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var desc Description
			if err := json.Unmarshal(first, &desc); err != nil {
				t.Fatalf("Unmarshal(%s): %v", first, err)
			}
			// What matters on the way back out is that the description still
			// says what it said: a shard is read and rewritten, and the second
			// meta.json must not be poorer than the first.
			second, err := json.Marshal(desc)
			if err != nil {
				t.Fatalf("Marshal after Unmarshal: %v", err)
			}
			if string(second) != string(first) {
				t.Errorf("round trip changed the description:\n first = %s\nsecond = %s", first, second)
			}
		})
	}
}

// TestIdentifyWithProseDescription is the failure as it actually arrived: a
// meta.json holding an Identify with one oai-identifier description and one of
// plain prose, which is legal enough for the endpoint to serve and used to be
// unreadable here.
func TestIdentifyWithProseDescription(t *testing.T) {
	const meta = `{
	  "repositoryName": "ISTA Publications",
	  "baseURL": "https://research-explorer.ista.ac.at/oai",
	  "protocolVersion": "2.0",
	  "description": [
	    {
	      "oai-identifier": {
	        "delimiter": ":",
	        "repositoryIdentifier": "pub.research-explorer.ista.ac.at",
	        "scheme": "oai"
	      }
	    },
	    "ISTA Research Explorer (ISTA REx) is the institutional repository."
	  ]
	}`

	var id Identify
	if err := json.Unmarshal([]byte(meta), &id); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if id.RepositoryName != "ISTA Publications" {
		t.Errorf("RepositoryName = %q", id.RepositoryName)
	}
	if len(id.Description) != 2 {
		t.Fatalf("got %d descriptions, want 2", len(id.Description))
	}
	// The prose is the inner XML of that element, so it survives as the body.
	if got := string(id.Description[1].Body); !strings.HasPrefix(got, "ISTA Research Explorer") {
		t.Errorf("Description[1].Body = %q, want the prose", got)
	}

	out, err := json.Marshal(id.Description)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{"pub.research-explorer.ista.ac.at", "ISTA Research Explorer"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rewriting the meta lost %q: %s", want, out)
		}
	}
}

// TestDescriptionFromXMLIsUnaffected: the harvest path builds descriptions by
// decoding XML, never JSON, and must marshal exactly as it did before there was
// an unmarshaller at all.
func TestDescriptionFromXMLIsUnaffected(t *testing.T) {
	const doc = `<Identify><description><toolkit><title>jOAI</title></toolkit></description></Identify>`
	var id Identify
	if err := xml.Unmarshal([]byte(doc), &id); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	b, err := json.Marshal(id.Description)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `[{"toolkit":{"title":"jOAI"}}]`; string(b) != want {
		t.Errorf("Marshal = %s, want %s", b, want)
	}
}
