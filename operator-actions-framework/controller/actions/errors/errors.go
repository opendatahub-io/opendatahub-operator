package errors

import (
	"fmt"
	"time"
)

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

type RequeueAfterError struct {
	After time.Duration
}

func (e RequeueAfterError) Error() string {
	return fmt.Sprintf("requeue after %s", e.After)
}

func NewRequeueAfterError(d time.Duration) RequeueAfterError {
	return RequeueAfterError{After: d}
}
