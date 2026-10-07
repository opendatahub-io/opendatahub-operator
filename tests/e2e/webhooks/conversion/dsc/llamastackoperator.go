package dsc

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
)

func (m LlamaStackOperatorSuite) run(t *testing.T) {
	m.runRetiredOperator(t, "llamastackoperator", func(components *dscv2.Components, state operatorv1.ManagementState) {
		components.LlamaStackOperator.ManagementState = state
	})
}
