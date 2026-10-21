package service

import "time"

// Clock abstracts the system clock so that time-based business rules can be
// tested deterministically. The service never calls time.Now directly.
type Clock interface {
	Now() time.Time
}

// systemClock reports the real wall clock.
type systemClock struct{}

// Now returns the current time.
func (systemClock) Now() time.Time { return time.Now() }

// NewClock returns the real system clock.
func NewClock() Clock { return systemClock{} }

// FixedClock always reports the same instant. It exists so that tests can
// pin "now" and assert on exact boundaries such as "exactly one hour away".
type FixedClock struct {
	T time.Time
}

// Now returns the pinned instant.
func (f FixedClock) Now() time.Time { return f.T }
