// C shim for libevdi.
//
// It exists for one reason: libevdi's event handlers take structs by value
// (struct evdi_mode in particular), and cgo cannot export a Go function with
// that signature. The handlers are written in C and forward flattened
// arguments to Go.
//
// The declarations here are this project's own. libevdi's header is LGPL-2.1;
// this project links the library dynamically and copies none of it, which is
// what that licence permits.

#ifndef TURZX_EVDI_SHIM_H
#define TURZX_EVDI_SHIM_H

#include <stdint.h>
#include <evdi_lib.h>

// turzx_evdi_fill_context populates an event context with our handlers.
// Cursor, crtc and stereo handlers are deliberately left null: this needs
// pixels and nothing else, and every handler omitted is one fewer by-value
// struct crossing the C/Go boundary.
void turzx_evdi_fill_context(struct evdi_event_context *ctx);

// turzx_evdi_wait blocks until the handle has events or the timeout expires.
// Returns more than zero if there are events to dispatch, zero on timeout, and
// negative if the wait itself failed, in which case the caller should fall back
// to sleeping rather than spinning.
int turzx_evdi_wait(evdi_handle handle, int timeout_ms);

// turzx_evdi_grab asks the library to copy the updated pixels into the buffer
// registered with it, and returns how many dirty rectangles were reported.
//
// Zero means nothing was grabbed, which libevdi also uses to signal a mode
// change rather than an empty frame. There is no destination argument because
// the pixels land in the buffer this process already allocated and registered,
// so the memory is already ours.
int turzx_evdi_grab(evdi_handle handle);

// turzx_evdi_version writes the library version into buf, returning its length.
int turzx_evdi_version(char *buf, int buf_len);

#endif
