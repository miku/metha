package metha

import (
	"bytes"
	"fmt"
)

type MultiError struct {
	Errors []error
}

func (e *MultiError) Error() string {
	var buf bytes.Buffer
	for i, err := range e.Errors {
		buf.WriteString(fmt.Sprintf("[%d] %s", i, err.Error()))
	}
	return buf.String()
}
