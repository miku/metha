package metha

import (
	"bytes"
	"fmt"
)

// MultiError collects a number of errors.
type MultiError struct {
	Errors []error
}

// Error formats all error strings into a single string.
func (e *MultiError) Error() string {
	var buf bytes.Buffer
	for i, err := range e.Errors {
		buf.WriteString(fmt.Sprintf("[%d] %s\n", i, err.Error()))
	}
	return buf.String()
}
