package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// writeJSON emits one object per line, for piping onward.
func writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", b)
	return nil
}

// duration renders a span at a precision that keeps it meaningful: rounding a
// fast harvest to the second prints "0s" beside a rate of megabytes per second.
func duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(10 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

// plural gives a count its noun.
func plural(n int, noun string) string {
	switch {
	case n == 1:
		return noun
	case strings.HasSuffix(noun, "y"):
		return noun[:len(noun)-1] + "ies"
	default:
		return noun + "s"
	}
}

// dash renders an empty string as a dash, so columns stay readable.
func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// thousands groups a count so that six figures can be read at a glance. A sweep
// reports numbers in the hundreds of thousands, and "244346" beside "24434" is
// a difference nobody sees at the end of a long day.
func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

// humanBytes renders a byte count in units a person can read.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
