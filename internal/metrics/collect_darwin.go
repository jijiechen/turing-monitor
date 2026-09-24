//go:build darwin

package metrics

import (
	"os"
	"time"
)

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
#include <mach/processor_info.h>
#include <mach/vm_statistics.h>
#include <sys/sysctl.h>
#include <sys/mount.h>
#include <sys/socket.h>
#include <net/route.h>
#include <net/if.h>
#include <net/if_var.h>
#include <netinet/in.h>
#include <stdlib.h>
#include <string.h>

// get_boottime reads the kernel boot timestamp.
static int get_boottime(struct timeval *tv) {
	size_t len = sizeof(*tv);
	return sysctlbyname("kern.boottime", tv, &len, NULL, 0);
}

// get_loadavg reads the 1/5/15 minute load averages.
static int get_loadavg(double *out) {
	struct loadavg la;
	size_t len = sizeof(la);
	if (sysctlbyname("vm.loadavg", &la, &len, NULL, 0) != 0) return -1;
	for (int i = 0; i < 3; i++) out[i] = (double)la.ldavg[i] / (double)la.fscale;
	return 0;
}

static int get_memsize(uint64_t *out) {
	size_t len = sizeof(*out);
	return sysctlbyname("hw.memsize", out, &len, NULL, 0);
}

static int get_swap(uint64_t *total, uint64_t *avail) {
	struct xsw_usage xu;
	size_t len = sizeof(xu);
	if (sysctlbyname("vm.swapusage", &xu, &len, NULL, 0) != 0) return -1;
	*total = xu.xsu_total;
	*avail = xu.xsu_avail;
	return 0;
}

// get_vm reads the VM statistics and the system page size.
static int get_vm(vm_statistics64_data_t *vm, uint64_t *pagesize) {
	mach_port_t host = mach_host_self();
	mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
	if (host_statistics64(host, HOST_VM_INFO64, (host_info64_t)vm, &count) != KERN_SUCCESS) return -1;
	vm_size_t ps = 0;
	if (host_page_size(host, &ps) != KERN_SUCCESS) return -1;
	*pagesize = (uint64_t)ps;
	return 0;
}

// get_cpu sums the per-core CPU tick counters.
static int get_cpu(uint64_t *user, uint64_t *system, uint64_t *idle, uint64_t *nice) {
	natural_t ncpu = 0;
	processor_info_array_t info = NULL;
	mach_msg_type_number_t nind = 0;
	if (host_processor_info(mach_host_self(), PROCESSOR_CPU_LOAD_INFO,
	                        &ncpu, &info, &nind) != KERN_SUCCESS) {
		return -1;
	}
	*user = *system = *idle = *nice = 0;
	processor_cpu_load_info_t loads = (processor_cpu_load_info_t)info;
	for (natural_t i = 0; i < ncpu; i++) {
		*user   += loads[i].cpu_ticks[CPU_STATE_USER];
		*system += loads[i].cpu_ticks[CPU_STATE_SYSTEM];
		*idle   += loads[i].cpu_ticks[CPU_STATE_IDLE];
		*nice   += loads[i].cpu_ticks[CPU_STATE_NICE];
	}
	vm_deallocate(mach_task_self(), (vm_address_t)info, nind * sizeof(integer_t));
	return 0;
}

// get_net sums 64-bit byte counters across all non-loopback interfaces.
// The older if_data fields wrap at 4 GiB, so the routing table's 64-bit
// variant is used instead.
static int get_net(uint64_t *rx, uint64_t *tx) {
	int mib[] = {CTL_NET, PF_ROUTE, 0, 0, NET_RT_IFLIST2, 0};
	size_t len = 0;
	if (sysctl(mib, 6, NULL, &len, NULL, 0) != 0) return -1;

	char *buf = malloc(len);
	if (buf == NULL) return -1;
	if (sysctl(mib, 6, buf, &len, NULL, 0) != 0) { free(buf); return -1; }

	*rx = 0; *tx = 0;
	char *limit = buf + len;
	for (char *next = buf; next < limit; ) {
		struct if_msghdr *ifm = (struct if_msghdr *)next;
		if (ifm->ifm_msglen == 0) break;
		next += ifm->ifm_msglen;
		if (ifm->ifm_type != RTM_IFINFO2) continue;
		struct if_msghdr2 *ifm2 = (struct if_msghdr2 *)ifm;
		if (ifm2->ifm_flags & IFF_LOOPBACK) continue;
		*rx += ifm2->ifm_data.ifi_ibytes;
		*tx += ifm2->ifm_data.ifi_obytes;
	}
	free(buf);
	return 0;
}

// get_root_fs reports usage of the root filesystem.
static int get_root_fs(uint64_t *total, uint64_t *avail) {
	struct statfs fs;
	if (statfs("/", &fs) != 0) return -1;
	*total = (uint64_t)fs.f_blocks * (uint64_t)fs.f_bsize;
	*avail = (uint64_t)fs.f_bavail * (uint64_t)fs.f_bsize;
	return 0;
}
*/
import "C"

// Collect reads one snapshot of the system.
//
// CPU temperature is not reported on macOS: Apple Silicon exposes it only
// through the SMC, which requires IOKit access that is not reliably available
// to an unsigned command-line tool.
func Collect() (Snapshot, error) {
	s := Snapshot{Time: time.Now()}
	s.Hostname, _ = os.Hostname()

	var tv C.struct_timeval
	if C.get_boottime(&tv) == 0 {
		s.Uptime = time.Since(time.Unix(int64(tv.tv_sec), int64(tv.tv_usec)*1000))
	}

	var la [3]C.double
	if C.get_loadavg(&la[0]) == 0 {
		s.Load = [3]float64{float64(la[0]), float64(la[1]), float64(la[2])}
	}

	var user, system, idle, nice C.uint64_t
	if C.get_cpu(&user, &system, &idle, &nice) == 0 {
		s.CPU = CPUTimes{
			User:   uint64(user),
			System: uint64(system),
			Idle:   uint64(idle),
			Nice:   uint64(nice),
		}
	}

	var vm C.vm_statistics64_data_t
	var pagesize C.uint64_t
	if C.get_vm(&vm, &pagesize) == 0 {
		page := uint64(pagesize)
		// Match how Activity Monitor defines "Memory Used": resident plus
		// compressed, excluding cached file pages.
		used := (uint64(vm.active_count) + uint64(vm.wire_count) + uint64(vm.compressor_page_count)) * page

		var total C.uint64_t
		if C.get_memsize(&total) == 0 {
			totalBytes := uint64(total)
			if used > totalBytes {
				used = totalBytes
			}
			s.Mem = MemInfo{Total: totalBytes, Used: used, Available: totalBytes - used}
		}
	}

	var swapTotal, swapAvail C.uint64_t
	if C.get_swap(&swapTotal, &swapAvail) == 0 {
		total := uint64(swapTotal)
		avail := uint64(swapAvail)
		if avail > total {
			avail = total
		}
		s.Swap = MemInfo{Total: total, Used: total - avail, Available: avail}
	}

	var rx, tx C.uint64_t
	if C.get_net(&rx, &tx) == 0 {
		s.Net = NetCounters{RxBytes: uint64(rx), TxBytes: uint64(tx)}
	}

	var fsTotal, fsAvail C.uint64_t
	if C.get_root_fs(&fsTotal, &fsAvail) == 0 {
		total := uint64(fsTotal)
		avail := uint64(fsAvail)
		if avail > total {
			avail = total
		}
		s.Mounts = []Mount{{Path: "/", Total: total, Used: total - avail}}
	}

	return s, nil
}
