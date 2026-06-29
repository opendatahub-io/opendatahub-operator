package errors

import (
	"fmt"
	"time"
)

// StopError is a marker error that thew ComponentController uses
// to break out from the action execution loop.
type StopError struct {
	reason error
}

func (e StopError) Error() string {
	return e.reason.Error()
}

func NewStopErrorW(reason error) StopError {
	return StopError{reason}
}
func NewStopError(format string, args ...any) StopError {
	return StopError{
		fmt.Errorf(format, args...),
	}
}

// RequeueAfterError is a marker error that tells the reconciler to
// requeue the reconciliation after the specified duration instead of
// treating it as a failure. Used by DAG gating when a runlevel is
// blocked waiting for a timeout to expire.
type RequeueAfterError struct {
	After time.Duration
}

func (e RequeueAfterError) Error() string {
	return fmt.Sprintf("requeue after %s", e.After)
}

func NewRequeueAfterError(d time.Duration) RequeueAfterError {
	return RequeueAfterError{After: d}
}
