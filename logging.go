package perimorph

import (
	"flag"
	"log"
	"os"
)

func init() {
	logFile := flag.String("log", "", "filename to log to")

	if *logFile != "" {
		file, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %s", err)
		}
		log.SetOutput(file)
	}
}
