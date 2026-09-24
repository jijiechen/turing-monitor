// Package metrics collects system statistics for the panel's dashboard mode.
//
// Collection is platform-specific (procfs on Linux, Mach host statistics on
// macOS) but everything downstream — rate computation, rendering, transport —
// is shared.
package metrics

import (
	"time"
)

// CPUTimes holds cumulative CPU time counters, in units of clock ticks. Only
// differences between two snapshots are meaningful.
type CPUTimes struct {
	User, Nice, System, Idle, IOWait, IRQ, SoftIRQ, Steal uint64
}

// Total returns the sum of all CPU time counters.
func (c CPUTimes) Total() uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal
}

// Busy returns the CPU time that was not idle, counting I/O wait as busy since
// that is time the CPU could not run user work.
func (c CPUTimes) Busy() uint64 {
	return c.Total() - c.Idle
}

// MemInfo describes memory usage in bytes.
type MemInfo struct {
	Total     uint64
	Used      uint64
	Available uint64
}

// UsedPercent returns the fraction of memory in use, 0-100.
func (m MemInfo) UsedPercent() float64 {
	if m.Total == 0 {
		return 0
	}
	return float64(m.Used) / float64(m.Total) * 100
}

// NetCounters holds cumulative network byte counts.
type NetCounters struct {
	RxBytes uint64
	TxBytes uint64
}

// Sensor is one temperature reading.
type Sensor struct {
	Name    string
	Celsius float64
}

// Mount is one filesystem's usage.
type Mount struct {
	Path  string
	Total uint64
	Used  uint64
}

// UsedPercent returns the fraction of the filesystem in use, 0-100.
func (m Mount) UsedPercent() float64 {
	if m.Total == 0 {
		return 0
	}
	return float64(m.Used) / float64(m.Total) * 100
}

// Snapshot is one complete reading of the system. Cumulative counters need a
// previous snapshot to become rates; everything else is meaningful on its own.
type Snapshot struct {
	Time     time.Time
	Hostname string
	Uptime   time.Duration
	Load     [3]float64
	CPU      CPUTimes
	Mem      MemInfo
	Swap     MemInfo
	Net      NetCounters
	Temps    []Sensor
	Mounts   []Mount
}

// Rates are the deltas derived from two snapshots.
type Rates struct {
	CPUPercent float64
	RxPerSec   float64
	TxPerSec   float64
}

// Delta computes rates between two snapshots. It returns zero rates when the
// snapshots are out of order or the interval is not positive, so a bad sample
// shows as a gap rather than a bogus spike.
func Delta(prev, cur Snapshot) Rates {
	elapsed := cur.Time.Sub(prev.Time).Seconds()
	if elapsed <= 0 {
		return Rates{}
	}

	var r Rates

	// CPU percentages come from the change in busy time over total time.
	busy := cur.CPU.Busy() - prev.CPU.Busy()
	total := cur.CPU.Total() - prev.CPU.Total()
	if total > 0 && cur.CPU.Total() >= prev.CPU.Total() {
		r.CPUPercent = float64(busy) / float64(total) * 100
	}

	// Network counters wrap at 32 bits on some platforms; treat a decrease as
	// a counter reset rather than a negative rate.
	if cur.Net.RxBytes >= prev.Net.RxBytes {
		r.RxPerSec = float64(cur.Net.RxBytes-prev.Net.RxBytes) / elapsed
	}
	if cur.Net.TxBytes >= prev.Net.TxBytes {
		r.TxPerSec = float64(cur.Net.TxBytes-prev.Net.TxBytes) / elapsed
	}
	return r
}
