//go:build windows

package main

import "os"

// acquireLock is a no-op on Windows: metha-pack has not been validated there
// and concurrent runs are the user's responsibility.
func acquireLock(path string) (*os.File, error) {
	return nil, nil
}
