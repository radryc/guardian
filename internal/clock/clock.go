// Package clock is the small time seam used by long-running loops so retry,
// lease and cooldown behaviour can be tested without wall-clock sleeps.
package clock

import "time"

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// Func adapts a function to Clock.
type Func func() time.Time

func (f Func) Now() time.Time { return f() }

// System reports the real current time.
var System Clock = Func(time.Now)

// Or returns c, or System when c is nil.
func Or(c Clock) Clock {
	if c == nil {
		return System
	}
	return c
}
