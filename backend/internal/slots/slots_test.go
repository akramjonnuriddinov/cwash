package slots

import (
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 11, 3, h, m, 0, 0, time.UTC) }

func base() Params {
	return Params{Open: at(9, 0), Close: at(12, 0), Step: 30 * time.Minute, Duration: 60 * time.Minute, Boxes: 1}
}

func fmtAll(ts []time.Time) []string {
	var s []string
	for _, t := range ts {
		s = append(s, t.Format("15:04"))
	}
	return s
}

func eq(t *testing.T, got []time.Time, want ...string) {
	t.Helper()
	g := fmtAll(got)
	if len(g) != len(want) {
		t.Fatalf("got %v want %v", g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("got %v want %v", g, want)
		}
	}
}

func TestEmptyDay(t *testing.T) {
	// 60-min service must end by 12:00 → last start 11:00
	eq(t, Free(base(), nil), "09:00", "09:30", "10:00", "10:30", "11:00")
}

func TestOneBoxBusy(t *testing.T) {
	busy := []Interval{{at(10, 0), at(10, 30)}}
	eq(t, Free(base(), busy), "09:00", "10:30", "11:00")
}

func TestTwoBoxes(t *testing.T) {
	p := base()
	p.Boxes = 2
	busy := []Interval{{at(10, 0), at(11, 0)}, {at(10, 30), at(11, 30)}}
	// 10:30-11:00 has 2 cars → any start covering that chunk is blocked
	eq(t, Free(p, busy), "09:00", "09:30", "11:00")
}

func TestNotBefore(t *testing.T) {
	p := base()
	p.NotBefore = at(10, 10)
	eq(t, Free(p, nil), "10:30", "11:00")
}

func TestUnalignedWalkin(t *testing.T) {
	busy := []Interval{{at(9, 10), at(9, 40)}}
	eq(t, Free(base(), busy), "10:00", "10:30", "11:00")
}
