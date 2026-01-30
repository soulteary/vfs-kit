package vfs

import (
	"errors"
	"testing"
)

func TestLogCloseErrorInjection(t *testing.T) {
	// LogCloseError is optional; callers may set it to log close errors. Default is no-op.
	called := false
	var captured error
	old := LogCloseError
	defer func() { LogCloseError = old }()
	LogCloseError = func(err error) {
		called = true
		captured = err
	}
	err := errors.New("close failed")
	LogCloseError(err)
	if !called {
		t.Error("LogCloseError was not called")
	}
	if captured != err {
		t.Errorf("LogCloseError captured %v, want %v", captured, err)
	}
}
