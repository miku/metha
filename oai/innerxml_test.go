package oai

import (
	"encoding/json"
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

// TestInnerXMLIsJSONNotBase64: the four fields holding raw XML are []byte, and
// encoding/json renders []byte as base64. Metadata had a marshaller and the
// other two did not, so "metha id" answered with a wall of base64 where a
// repository's oai-identifier and toolkit descriptions should have been, and
// the same for every set description.
func TestInnerXMLIsJSONNotBase64(t *testing.T) {
	const body = `<oai-identifier xmlns="http://www.openarchives.org/OAI/2.0/oai-identifier">` +
		`<scheme>oai</scheme><delimiter>:</delimiter></oai-identifier>`

	for _, tt := range []struct {
		name string
		v    any
	}{
		{"Description", Description{Body: []byte(body)}},
		{"About", About{Body: []byte(body)}},
		{"Metadata", Metadata{Body: []byte(body)}},
		// The shapes they are actually reached through in metha id and
		// metha cat --json, since a marshaller on the wrong type would look
		// right in isolation and still leave the output base64.
		{"Identify.Description", Identify{Description: []Description{{Body: []byte(body)}}}},
		{"Set.SetDescription", Set{SetDescription: Description{Body: []byte(body)}}},
		{"Record.About", Record{About: About{Body: []byte(body)}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.v)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got := string(b)
			// The base64 of that body, which is what used to come out.
			if strings.Contains(got, "PG9haS1pZGVudGlmaWVy") {
				t.Errorf("marshalled to base64: %s", got)
			}
			if !strings.Contains(got, `"scheme":"oai"`) {
				t.Errorf("the XML did not become JSON: %s", got)
			}
		})
	}
}

// TestInnerXMLSurvivesUnparseableBody: json.Marshal abandons the whole document
// when a field's marshaller returns an error, so one malformed metadata block
// used to fail an entire "metha cat --json" run. Over a corpus of this size
// there will be malformed blocks, and one bad record must cost one record.
func TestInnerXMLSurvivesUnparseableBody(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		// A body that will not parse comes back as a JSON string of itself,
		// so nothing is lost and nothing fails. json.Marshal escapes the
		// angle brackets, which is why this is compared after decoding.
		{"not XML at all", "not xml at all", `"not xml at all"`},
		{"a truncated element", "<dc:title>unclosed", `"<dc:title>unclosed"`},
		{"empty", "", "{}"},
		{"whitespace only", "\n\t\t", "{}"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(Metadata{Body: []byte(tt.body)})
			if err != nil {
				t.Fatalf("Marshal: %v, want the record to survive a body that will not parse", err)
			}
			// Compared after a decode, since json.Marshal escapes < and >
			// and the point is the value, not its spelling.
			var got, want any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("the output is not valid JSON: %s", b)
			}
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatalf("bad fixture %q: %v", tt.want, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Marshal = %v, want %v", got, want)
			}
		})
	}
}

// TestRecordJSONOmitsXMLName: XMLName carries the element a record was decoded
// from, and store.Render sets it before writing XML so the output is
// namespaced. In JSON it is an artefact of a different encoding - every line of
// "metha cat --json" used to open with the name and namespace of the element it
// is no longer in.
//
// Both halves are here because they share one field: dropping it outright would
// take the namespace off the XML output, which is the reason it exists.
func TestRecordJSONOmitsXMLName(t *testing.T) {
	rec := Record{
		XMLName: xml.Name{Local: "record", Space: "http://www.openarchives.org/OAI/2.0/"},
		Header:  Header{Identifier: "oai:example.org:1", DateStamp: "2023-01-01"},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(b); strings.Contains(got, "XMLName") || strings.Contains(got, "openarchives") {
		t.Errorf("JSON carries the XML element name: %s", got)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := back["header"]; !ok {
		t.Errorf("the record lost its header along with XMLName: %s", b)
	}

	// And the XML side still names the element and its namespace.
	x, err := xml.Marshal(rec)
	if err != nil {
		t.Fatalf("xml.Marshal: %v", err)
	}
	if want := `<record xmlns="http://www.openarchives.org/OAI/2.0/">`; !strings.HasPrefix(string(x), want) {
		t.Errorf("xml.Marshal = %s, want it to start with %s", x, want)
	}
}
