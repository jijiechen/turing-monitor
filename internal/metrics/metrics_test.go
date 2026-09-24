package metrics

import (
	"math"
	"testing"
	"time"
)

func TestCPUTimes(t *testing.T) {
	c := CPUTimes{User: 10, System: 5, Idle: 85}
	if got, want := c.Total(), uint64(100); got != want {
		t.Errorf("Total = %d, want %d", got, want)
	}
	if got, want := c.Busy(), uint64(15); got != want {
		t.Errorf("Busy = %d, want %d", got, want)
	}
}

func TestDeltaCPU(t *testing.T) {
	base := time.Unix(1000, 0)
	prev := Snapshot{Time: base, CPU: CPUTimes{User: 100, Idle: 100}}

	// Half of the elapsed CPU time was spent busy.
	cur := Snapshot{Time: base.Add(time.Second), CPU: CPUTimes{User: 150, Idle: 150}}

	if got := Delta(prev, cur).CPUPercent; math.Abs(got-50) > 0.001 {
		t.Errorf("CPUPercent = %v, want 50", got)
	}
}

func TestDeltaNetwork(t *testing.T) {
	base := time.Unix(1000, 0)
	prev := Snapshot{Time: base, Net: NetCounters{RxBytes: 1000, TxBytes: 2000}}
	cur := Snapshot{Time: base.Add(2 * time.Second), Net: NetCounters{RxBytes: 3000, TxBytes: 4000}}

	r := Delta(prev, cur)
	if got, want := r.RxPerSec, 1000.0; math.Abs(got-want) > 0.001 {
		t.Errorf("RxPerSec = %v, want %v", got, want)
	}
	if got, want := r.TxPerSec, 1000.0; math.Abs(got-want) > 0.001 {
		t.Errorf("TxPerSec = %v, want %v", got, want)
	}
}

func TestDeltaHandlesCounterReset(t *testing.T) {
	base := time.Unix(1000, 0)
	prev := Snapshot{Time: base, Net: NetCounters{RxBytes: 5000, TxBytes: 5000}}
	// The counter went backwards, as a 32-bit interface counter would on wrap.
	cur := Snapshot{Time: base.Add(time.Second), Net: NetCounters{RxBytes: 10, TxBytes: 10}}

	r := Delta(prev, cur)
	if r.RxPerSec != 0 || r.TxPerSec != 0 {
		t.Errorf("expected zero rates after a counter reset, got %+v", r)
	}
}

func TestDeltaRejectsNonPositiveInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	s := Snapshot{Time: now, CPU: CPUTimes{User: 100}}
	if r := Delta(s, s); r != (Rates{}) {
		t.Errorf("expected zero rates for a zero interval, got %+v", r)
	}
	// Out-of-order snapshots must not produce a bogus negative or huge rate.
	prev := Snapshot{Time: now.Add(time.Second), CPU: CPUTimes{User: 200}}
	if r := Delta(prev, s); r != (Rates{}) {
		t.Errorf("expected zero rates for reversed snapshots, got %+v", r)
	}
}

func TestUsedPercent(t *testing.T) {
	m := MemInfo{Total: 200, Used: 50}
	if got := m.UsedPercent(); math.Abs(got-25) > 0.001 {
		t.Errorf("UsedPercent = %v, want 25", got)
	}
	// A zero total must not divide by zero.
	if got := (MemInfo{}).UsedPercent(); got != 0 {
		t.Errorf("UsedPercent of empty MemInfo = %v, want 0", got)
	}
	if got := (Mount{}).UsedPercent(); got != 0 {
		t.Errorf("UsedPercent of empty Mount = %v, want 0", got)
	}
}

// TestCollect exercises the platform collector. It only asserts that the values
// are self-consistent, since they depend on the machine it runs on.
func TestCollect(t *testing.T) {
	s, err := Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if s.Time.IsZero() {
		t.Error("snapshot has no timestamp")
	}
	if s.Mem.Total == 0 {
		t.Error("total memory is zero")
	}
	if s.Mem.Used > s.Mem.Total {
		t.Errorf("used memory %d exceeds total %d", s.Mem.Used, s.Mem.Total)
	}
	if s.CPU.Total() == 0 {
		t.Error("CPU counters are all zero")
	}

	// A second sample must produce sane rates.
	time.Sleep(50 * time.Millisecond)
	s2, err := Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	r := Delta(s, s2)
	if r.CPUPercent < 0 || r.CPUPercent > 100 {
		t.Errorf("CPUPercent = %v, out of range", r.CPUPercent)
	}
	if r.RxPerSec < 0 || r.TxPerSec < 0 {
		t.Errorf("negative network rate: %+v", r)
	}
}
