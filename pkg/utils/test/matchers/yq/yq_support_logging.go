package yq

import (
	"gopkg.in/op/go-logging.v1"
)

type nullLogger struct {
	// disable logging for yq
}

func (l *nullLogger) Log(_ logging.Level, _ int, _ *logging.Record) error {
	return nil
}
