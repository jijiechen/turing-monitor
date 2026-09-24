//go:build linux

package metrics

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Collect reads one snapshot from procfs and sysfs.
func Collect() (Snapshot, error) {
	s := Snapshot{Time: time.Now()}
	s.Hostname, _ = os.Hostname()

	if up, err := readUptime(); err == nil {
		s.Uptime = up
	}
	if la, err := readLoadAvg(); err == nil {
		s.Load = la
	}
	if cpu, err := readCPU(); err == nil {
		s.CPU = cpu
	}
	if mem, swap, err := readMemInfo(); err == nil {
		s.Mem, s.Swap = mem, swap
	}
	if net, err := readNetDev(); err == nil {
		s.Net = net
	}
	s.Temps = readTemps()
	s.Mounts = readMounts()

	return s, nil
}

func readUptime() (time.Duration, error) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, fmt.Errorf("malformed /proc/uptime")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(secs * float64(time.Second)), nil
}

func readLoadAvg() ([3]float64, error) {
	var out [3]float64
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return out, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return out, fmt.Errorf("malformed /proc/loadavg")
	}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return out, err
		}
		out[i] = v
	}
	return out, nil
}

// readCPU sums the per-CPU counters on the aggregate "cpu" line of /proc/stat.
func readCPU() (CPUTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return CPUTimes{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var vals [8]uint64
		for i := 0; i < len(fields) && i < len(vals); i++ {
			v, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return CPUTimes{}, err
			}
			vals[i] = v
		}
		return CPUTimes{
			User: vals[0], Nice: vals[1], System: vals[2], Idle: vals[3],
			IOWait: vals[4], IRQ: vals[5], SoftIRQ: vals[6], Steal: vals[7],
		}, nil
	}
	return CPUTimes{}, fmt.Errorf("no aggregate cpu line in /proc/stat")
}

func readMemInfo() (MemInfo, MemInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemInfo{}, MemInfo{}, err
	}
	defer f.Close()

	kv := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		// Values are in kB.
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		kv[key] = v * 1024
	}

	// MemAvailable is the kernel's own estimate of what can be reclaimed and
	// is far more meaningful than MemFree.
	used := kv["MemTotal"] - kv["MemAvailable"]
	mem := MemInfo{Total: kv["MemTotal"], Used: used, Available: kv["MemAvailable"]}

	swapUsed := kv["SwapTotal"] - kv["SwapFree"]
	swap := MemInfo{Total: kv["SwapTotal"], Used: swapUsed, Available: kv["SwapFree"]}
	return mem, swap, nil
}

// readNetDev sums byte counters across every interface except loopback.
func readNetDev() (NetCounters, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return NetCounters{}, err
	}
	defer f.Close()

	var n NetCounters
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue // the two header lines have no colon
		}
		if strings.TrimSpace(name) == "lo" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		n.RxBytes += rx
		n.TxBytes += tx
	}
	return n, nil
}

// readTemps gathers thermal zone and hwmon temperature sensors, in Celsius.
func readTemps() []Sensor {
	var out []Sensor
	out = append(out, readThermalZones()...)
	out = append(out, readHwmon()...)
	return out
}

func readThermalZones() []Sensor {
	zones, err := filepath.Glob("/sys/class/thermal/thermal_zone*")
	if err != nil {
		return nil
	}
	var out []Sensor
	for _, z := range zones {
		milli, err := readIntFile(filepath.Join(z, "temp"))
		if err != nil {
			continue
		}
		name := readStringFile(filepath.Join(z, "type"))
		if name == "" {
			name = filepath.Base(z)
		}
		out = append(out, Sensor{Name: name, Celsius: float64(milli) / 1000})
	}
	return out
}

func readHwmon() []Sensor {
	dirs, err := filepath.Glob("/sys/class/hwmon/hwmon*")
	if err != nil {
		return nil
	}
	var out []Sensor
	for _, d := range dirs {
		label := readStringFile(filepath.Join(d, "name"))
		inputs, _ := filepath.Glob(filepath.Join(d, "temp*_input"))
		for _, in := range inputs {
			milli, err := readIntFile(in)
			if err != nil {
				continue
			}
			name := label
			if name == "" {
				name = filepath.Base(d)
			}
			out = append(out, Sensor{Name: name, Celsius: float64(milli) / 1000})
		}
	}
	return out
}

func readMounts() []Mount {
	var out []Mount
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return nil
	}
	total := st.Blocks * uint64(st.Bsize)
	avail := st.Bavail * uint64(st.Bsize)
	out = append(out, Mount{Path: "/", Total: total, Used: total - avail})
	return out
}

func readIntFile(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}

func readStringFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
