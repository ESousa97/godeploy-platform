// Package iox provides tiny I/O helpers shared across godeploy, such as best-effort
// [Close] for values that implement [io.Closer].
package iox

import "io"

// Close calls c.Close when c is non-nil. Errors from Close are ignored; use
// only when there is no corrective action (typical defer after successful setup).
func Close(c io.Closer) {
	if c == nil {
		return
	}
	_ = c.Close() //nolint:errcheck // best-effort close
}
