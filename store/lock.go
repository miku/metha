package store

import "errors"

// LockName is the name of the lock file a shard keeps, so that two harvests of
// one endpoint cannot interleave. It deliberately does not match any pattern
// the readers look for, so it stays invisible to Files and Render.
const LockName = "LOCK"

// ErrLocked signals that another process holds the lock. Callers that can
// simply come back later (a harvest loop, a packing run) should treat this as
// "skip", not as a failure.
var ErrLocked = errors.New("locked by another process")
