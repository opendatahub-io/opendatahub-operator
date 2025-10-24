package parser

import (
	"fmt"

	"gotest.tools/gotestsum/testjson"
)

type eventHandler struct {
	formatter testjson.EventFormatter
}

func (h *eventHandler) Err(text string) error {
	// always return nil, no need to stop scanning if the stderr write fails
	return nil
}

func (h *eventHandler) Event(event testjson.TestEvent, execution *testjson.Execution) error {
	err := h.formatter.Format(event, execution)
	if err != nil {
		return fmt.Errorf("failed to format event: %w", err)
	}

	return nil
}

func (h *eventHandler) Flush() {}

func (h *eventHandler) Close() error {
	return nil
}
