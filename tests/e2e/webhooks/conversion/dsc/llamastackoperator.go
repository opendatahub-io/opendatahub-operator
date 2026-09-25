package dsc

import "testing"

func (m LlamaStackOperatorSuite) run(t *testing.T) {
	m.runRetiredOperator(t, "llamastackoperator")
}
