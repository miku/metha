package metha

import (
	"io/ioutil"
	"os"
	"sort"
)

// Extracts some maximum value as string.
type Laster interface {
	Last() (string, error)
}

// DirLaster extract the maximum value from the files of a directory. The values
// are extracted per file via TransformFunc, which gets a filename and returns a
// token. The tokens are sorted and the lexikographically largest element is
// returned.
type DirLaster struct {
	Dir           string
	DefaultValue  string
	ExtractorFunc func(os.FileInfo) string
}

// Last extracts the maximum value from a directory, given an extractor
// function.
func (l DirLaster) Last() (string, error) {
	files, err := ioutil.ReadDir(l.Dir)
	if err != nil {
		return "", err
	}
	var values []string
	for _, fi := range files {
		v := l.ExtractorFunc(fi)
		if v != "" {
			values = append(values, v)
		}
	}
	sort.Strings(values)
	if len(values) > 0 {
		return values[len(values)-1], nil
	}
	return l.DefaultValue, nil
}
