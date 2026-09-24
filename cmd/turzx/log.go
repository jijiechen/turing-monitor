package main

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"
)

// timestampWriter prefixes each complete line with the wall-clock time.
//
// The supervisor's messages are the only record of a reconnect, and the number
// that matters when deciding whether an unattended display feels responsive is
// how long the gap was. Without timestamps a log can show that a reconnect
// happened but not whether it took one second or ten.
//
// It buffers until it sees a newline so a line written in several pieces is
// still stamped once.
type timestampWriter struct {
	w  io.Writer
	mu sync.Mutex

	// pending holds a partial line, i.e. whatever followed the last newline.
	pending []byte
}

func newTimestampWriter(w io.Writer) io.Writer {
	return &timestampWriter{w: w}
}

func (t *timestampWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pending = append(t.pending, p...)
	for {
		idx := bytes.IndexByte(t.pending, '\n')
		if idx < 0 {
			break
		}
		line := t.pending[:idx]
		// Copy rather than reslice, so the rest of the buffer is not aliased
		// by what we just wrote.
		t.pending = append([]byte(nil), t.pending[idx+1:]...)

		stamp := time.Now().Format("15:04:05.000")
		if _, err := fmt.Fprintf(t.w, "%s  %s\n", stamp, line); err != nil {
			// Report the write as consumed anyway: the caller is a logger and
			// has nothing useful to do about a broken log.
			return len(p), err
		}
	}
	return len(p), nil
}
