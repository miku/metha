package metha

import "errors"

// LockName is the name of the lock file kept inside a harvesting directory.
// It deliberately does not match any pattern the readers glob for, so it stays
// invisible to Files, Render and metha-pack.
const LockName = "LOCK"

// ErrLocked signals that another process holds the lock. Callers that can
// simply come back later (a harvest loop, a packing run) should treat this as
// "skip", not as a failure.
var ErrLocked = errors.New("locked by another process")
