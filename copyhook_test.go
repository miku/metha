package metha

import (
	"bytes"
	"io/ioutil"
	"testing"

	log "github.com/sirupsen/logrus"
)

type testformatter struct{}

func (f *testformatter) Format(e *log.Entry) ([]byte, error) {
	return []byte(e.Message), nil
}

func TestCopyHook(t *testing.T) {
	w := &bytes.Buffer{}

	log.SetFormatter(new(testformatter))
	log.SetOutput(ioutil.Discard)
	log.AddHook(NewCopyHook(w, log.InfoLevel))

	exp := "A"
	log.Info(exp)
	log.Warn("B")

	if got := w.String(); got != exp {
		t.Errorf("Expected '%s', got '%s'", exp, got)
	}
}
