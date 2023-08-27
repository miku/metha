package metha

import (
	"io"

	log "github.com/sirupsen/logrus"
)

// CopyHook is a Logrus hook that copies messages to a writer.
type CopyHook struct {
	io.Writer
	levels []log.Level
}

// NewCopyHook initializes a copy hook. By default, it copies Warn, Error, Fatal
// and Panic level messages. Override these by passing in other logrus.Level
// values.
func NewCopyHook(w io.Writer, levels ...log.Level) CopyHook {
	ch := CopyHook{
		Writer: w,
	}
	if len(levels) > 0 {
		ch.levels = levels
	} else {
		ch.levels = []log.Level{
			log.WarnLevel,
			log.ErrorLevel,
			log.FatalLevel,
			log.PanicLevel,
		}
	}
	return ch
}

// Levels returns the levels the CopyLogger logs.
func (hook CopyHook) Levels() []log.Level {
	return hook.levels
}

// Fire writes a logrus message.
func (hook CopyHook) Fire(entry *log.Entry) error {
	line, err := entry.String()
	if err != nil {
		return err
	}
	for _, l := range hook.levels {
		if l == entry.Level {
			_, err := hook.Write([]byte(line))
			return err
		}
	}
	return nil
}
