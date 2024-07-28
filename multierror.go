package metha

import (
	"bytes"
	"fmt"
	"io"
)

// MultiError collects a number of errors.
type MultiError struct {
	Errors []error
}

// Error formats all error strings into a single string.
func (e *MultiError) Error() string {
	var buf bytes.Buffer
	_, _ = io.WriteString(&buf, fmt.Sprintf("%d errors encountered:\n", len(e.Errors)))
	for _, err := range e.Errors {
		buf.WriteString(fmt.Sprintf("[E] %s\n", err.Error()))
	}
	return buf.String()
}
