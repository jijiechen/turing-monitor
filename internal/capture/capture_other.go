//go:build !darwin && !linux

package capture

import (
	"fmt"
	"runtime"
)

// Session is unused on platforms without a capture backend.
type Session struct{}

// Start reports that capture is not supported here.
func Start(opts Options) (*Session, error) {
	return nil, fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}

// Next is never reached.
func (s *Session) Next() ([]byte, error) { return nil, ErrUnsupported }

// Close is never reached.
func (s *Session) Close() error { return nil }
