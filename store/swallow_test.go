package store

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/miku/metha/oai"
)

// truncatedThenMore builds what a segment looks like when one stored response
// was cut off in flight: a document that opens a record and never closes it,
// followed by however many whole responses came after it.
//
// The decoder is lenient by design - the cache holds what endpoints actually
// sent - so the unterminated record does not end where the document does. It
// absorbs everything after it as its own inner XML.
func truncatedThenMore(docs int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>`)
	b.WriteString(`<record><header><identifier>cut</identifier></header><metadata><dc>`)
	for i := range docs {
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>`)
		for j := range 50 {
			fmt.Fprintf(&b, `<record><header><identifier>ok-%d-%d</identifier>`+
				`<datestamp>2023-01-15</datestamp></header>`+
				`<metadata><dc><title>%s</title></dc></metadata></record>`,
				i, j, strings.Repeat("t", 500))
		}
		b.WriteString(`</ListRecords></OAI-PMH>`)
	}
	return b.String()
}

// readAll runs the stream through the reader the way recordsFromExtent does,
// with the budget in place, and reports what came out and what it cost.
func readAll(t *testing.T, in string, max int) (records int, alloc uint64, err error) {
	t.Helper()
	opts := ReadOptions{MaxRecordBytes: max}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	recordsFromXML(newRecordScanner(strings.NewReader(in), max), "probe", opts,
		func(rec oai.Record, e error) bool {
			if e != nil {
				err = e
				return true
			}
			records++
			return true
		})
	runtime.ReadMemStats(&after)
	return records, after.TotalAlloc - before.TotalAlloc, err
}

// TestTruncatedRecordDoesNotSwallowTheExtent is the OOM, as a test.
//
// The size check that runs after DecodeElement cannot help here, because
// DecodeElement is what does not return: it goes on absorbing the rest of the
// extent into one record. Before the budget, this allocated about five times
// the input no matter what MaxRecordBytes said - so a multi-gigabyte shard cost
// multiple gigabytes of memory, and the run was killed rather than reporting
// anything.
//
// What must hold now is that the cost stops tracking the input.
func TestTruncatedRecordDoesNotSwallowTheExtent(t *testing.T) {
	// Small enough that both inputs are well past the budget, which is the
	// comparison that means anything: an input that fits inside the budget
	// simply runs to EOF, and finding that out says nothing about bounding.
	const max = 64 << 10

	small := truncatedThenMore(200)  // about 6MB
	large := truncatedThenMore(2000) // ten times that

	_, smallAlloc, smallErr := readAll(t, small, max)
	_, largeAlloc, largeErr := readAll(t, large, max)

	t.Logf("input %9d B -> allocated %9d B, err=%v", len(small), smallAlloc, smallErr)
	t.Logf("input %9d B -> allocated %9d B, err=%v", len(large), largeAlloc, largeErr)

	for _, err := range []error{smallErr, largeErr} {
		if !errors.Is(err, ErrRecordTooLarge) {
			t.Errorf("err = %v, want ErrRecordTooLarge", err)
		}
	}
	// The whole point. Before the budget this allocated about five times the
	// input whatever MaxRecordBytes said, so ten times the extent cost ten
	// times the memory and a large shard took the process with it. The cost has
	// to follow the budget now, not the file.
	if ratio := float64(largeAlloc) / float64(smallAlloc); ratio > 2 {
		t.Errorf("input grew %.0fx and allocation grew %.1fx; the budget is not bounding the decode",
			float64(len(large))/float64(len(small)), ratio)
	}
}

// TestUnboundedStillSwallows records the behaviour the budget is switched off
// for. MaxRecordBytes of zero means no bound anywhere, which is what metha cat
// defaults to, and the cost there is the caller's to accept.
func TestUnboundedStillSwallows(t *testing.T) {
	in := truncatedThenMore(200)
	_, alloc, err := readAll(t, in, 0)
	t.Logf("unbounded: input %d B -> allocated %d B, err=%v", len(in), alloc, err)
	if errors.Is(err, ErrRecordTooLarge) {
		t.Error("the budget fired with MaxRecordBytes at zero")
	}
}

// TestBudgetLeavesGoodRecordsAlone: an extent of ordinary responses has to read
// exactly as it did before, with every record delivered. A bound that costs
// records in the normal case would be worse than the crash it prevents.
func TestBudgetLeavesGoodRecordsAlone(t *testing.T) {
	var b strings.Builder
	const want = 500
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>`)
	for i := range want {
		fmt.Fprintf(&b, `<record><header><identifier>id-%d</identifier>`+
			`<datestamp>2023-01-15</datestamp></header>`+
			`<metadata><dc><title>%s</title></dc></metadata></record>`,
			i, strings.Repeat("t", 2000))
	}
	b.WriteString(`</ListRecords></OAI-PMH>`)

	got, _, err := readAll(t, b.String(), 1<<20)
	if err != nil {
		t.Fatalf("err = %v, want none", err)
	}
	if got != want {
		t.Errorf("got %d records, want %d", got, want)
	}
}

// TestBudgetAllowsARecordUpToTheLimit keeps recordBudget honest: a record just
// under MaxRecordBytes must decode, or the backstop would be firing on records
// the cheap check exists to handle.
func TestBudgetAllowsARecordUpToTheLimit(t *testing.T) {
	const max = 1 << 20
	body := strings.Repeat("t", max-4096)
	in := `<?xml version="1.0" encoding="UTF-8"?><OAI-PMH><ListRecords>` +
		`<record><header><identifier>big</identifier><datestamp>2023-01-15</datestamp></header>` +
		`<metadata><dc><title>` + body + `</title></dc></metadata></record>` +
		`</ListRecords></OAI-PMH>`

	got, _, err := readAll(t, in, max)
	if err != nil {
		t.Fatalf("err = %v, want none: the budget fired on a record inside the limit", err)
	}
	if got != 1 {
		t.Errorf("got %d records, want 1", got)
	}
}
