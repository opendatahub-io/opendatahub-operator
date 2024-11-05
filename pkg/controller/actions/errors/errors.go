package errors

import (
	"fmt"
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
