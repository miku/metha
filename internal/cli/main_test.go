package cli

import (
	"testing"

	"github.com/miku/metha"
	"github.com/miku/metha/oai"
)

// TestSetUserAgent: the release build injects the version into package metha,
// and the protocol package builds the User-Agent out of it. The wiring is one
// call in Main, which is easy to delete by accident and silent when it is gone
// - endpoints would just start seeing a version-less agent.
func TestSetUserAgent(t *testing.T) {
	before := oai.DefaultUserAgent
	t.Cleanup(func() { oai.DefaultUserAgent = before })
	setUserAgent()
	if want := "metha/" + metha.Version; oai.DefaultUserAgent != want {
		t.Errorf("oai.DefaultUserAgent = %q, want %q", oai.DefaultUserAgent, want)
	}
}
