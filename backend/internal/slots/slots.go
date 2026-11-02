// Package slots computes free booking start times given box capacity.
package slots

import "time"

type Interval struct{ Start, End time.Time }

type Params struct {
	Open, Close time.Time     // working window for the day
	Step        time.Duration // grid step (e.g. 30m)
	Duration    time.Duration // service duration
	Boxes       int           // parallel capacity
	NotBefore   time.Time     // hide starts earlier than this (now + buffer)
}

// Free returns every grid start time where the service fits fully inside
// working hours and capacity is never exceeded.
func Free(p Params, busy []Interval) []time.Time {
	var out []time.Time
	for t := p.Open; !t.Add(p.Duration).After(p.Close); t = t.Add(p.Step) {
		if t.Before(p.NotBefore) {
			continue
		}
		if Fits(t, t.Add(p.Duration), p.Step, p.Boxes, busy) {
			out = append(out, t)
		}
	}
	return out
}

// Fits reports whether [start,end) can take one more car. Time is checked in
// step-sized chunks; an existing order counts against every chunk it touches
// (conservative for unaligned walk-ins, never overbooks).
func Fits(start, end time.Time, step time.Duration, boxes int, busy []Interval) bool {
	for s := start; s.Before(end); s = s.Add(step) {
		e := s.Add(step)
		if e.After(end) {
			e = end
		}
		n := 0
		for _, b := range busy {
			if b.Start.Before(e) && b.End.After(s) {
				n++
			}
		}
		if n >= boxes {
			return false
		}
	}
	return true
}
