package cli

import (
	"bytes"
	"io"
	stdlog "log"
)

// httpNoise are messages the standard library writes about a peer that is
// broken in a way we can do nothing about and do not need to hear.
//
// net/http reports these with log.Printf from inside the transport, not as an
// error on any request: by the time they are written the connection has already
// been taken out of the pool and whatever request touched it has already
// returned its own result. They are unactionable here - the fault is on the
// far end - and the long tail of OAI endpoints produces them steadily.
//
// Matched as a substring against one Write, which is safe because log.Logger
// assembles a whole line and writes it in a single call.
var httpNoise = [][]byte{
	// A server wrote bytes onto an idle keep-alive connection: a body whose
	// length it got wrong, a late response to a request that was cancelled, or
	// TLS records on a connection it thinks is encrypted. The payload is
	// printed as a %q blob, so these lines are also the longest thing in the
	// log by a wide margin.
	[]byte("Unsolicited response received on idle HTTP channel"),
}

// noiseFilter drops the lines in httpNoise and passes everything else to w.
type noiseFilter struct {
	w io.Writer
}

func (f noiseFilter) Write(p []byte) (int, error) {
	for _, noise := range httpNoise {
		if bytes.Contains(p, noise) {
			return len(p), nil
		}
	}
	return f.w.Write(p)
}

// routeStdlibLog points the standard library's default logger at w, minus the
// noise.
//
// Everything metha logs goes through logrus, so log.SetOutput elsewhere in this
// package configures logrus and leaves the stdlib logger writing to stderr.
// That is how a sweep with its log discarded still prints transport complaints
// over the progress line. This is the other half: the destination the command
// chose, applied to the logger the standard library actually uses.
func routeStdlibLog(w io.Writer) {
	stdlog.SetOutput(noiseFilter{w: w})
}
