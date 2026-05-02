package iox

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type errCloser struct{ err error }

func (e errCloser) Close() error { return e.err }

func TestClose_nil(t *testing.T) {
	Close(nil)
}

func TestClose_ok(t *testing.T) {
	Close(io.NopCloser(strings.NewReader("")))
}

func TestClose_withError(t *testing.T) {
	Close(errCloser{err: errors.New("x")})
}
