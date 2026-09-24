#include "evdi_shim.h"

#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <sys/select.h>
#include <sys/time.h>

// The Go callbacks are declared by cgo itself. Declaring them by hand here
// would be wrong: cgo's integer types are not C's, so a hand-written prototype
// with int parameters would disagree with the generated definition and fail to
// link.
#include "_cgo_export.h"

static void on_mode_changed(struct evdi_mode mode, void *user_data) {
	(void)user_data;
	turzxEvdiModeChanged(mode.width, mode.height, mode.refresh_rate,
	                     mode.bits_per_pixel, mode.pixel_format);
}

static void on_update_ready(int buffer_to_be_updated, void *user_data) {
	(void)user_data;
	turzxEvdiUpdateReady(buffer_to_be_updated);
}

static void on_dpms(int dpms_mode, void *user_data) {
	(void)user_data;
	turzxEvdiDpms(dpms_mode);
}

void turzx_evdi_fill_context(struct evdi_event_context *ctx) {
	memset(ctx, 0, sizeof(*ctx));
	ctx->dpms_handler = on_dpms;
	ctx->mode_changed_handler = on_mode_changed;
	ctx->update_ready_handler = on_update_ready;
	ctx->user_data = NULL;
}

int turzx_evdi_wait(evdi_handle handle, int timeout_ms) {
	evdi_selectable fd = evdi_get_event_ready(handle);
	if (fd < 0) {
		return -1;
	}

	fd_set read_fds;
	FD_ZERO(&read_fds);
	FD_SET(fd, &read_fds);

	struct timeval tv;
	tv.tv_sec = timeout_ms / 1000;
	tv.tv_usec = (timeout_ms % 1000) * 1000;

	int rc = select(fd + 1, &read_fds, NULL, NULL, &tv);
	if (rc < 0 && errno != EINTR) {
		return -1;
	}
	return rc;
}

int turzx_evdi_grab(evdi_handle handle) {
	// libevdi fills up to 16 dirty rectangles. The count is in/out: the caller
	// states the capacity and the library reports how many it wrote.
	struct evdi_rect rects[16];
	int count = 16;

	evdi_grab_pixels(handle, rects, &count);
	return count;
}

int turzx_evdi_version(char *buf, int buf_len) {
	struct evdi_lib_version v;
	evdi_get_lib_version(&v);
	return snprintf(buf, (size_t)buf_len, "%d.%d.%d",
	                v.version_major, v.version_minor, v.version_patchlevel);
}
