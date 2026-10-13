package monitor_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"

	. "github.com/onsi/gomega"
)

func TestCheckResultConditionStatus(t *testing.T) {
	tests := []struct {
		name     string
		result   monitor.CheckResult
		expected metav1.ConditionStatus
	}{
		{name: "empty status derives true from pass", result: monitor.CheckResult{Pass: true}, expected: metav1.ConditionTrue},
		{name: "empty status derives false from failure", result: monitor.CheckResult{}, expected: metav1.ConditionFalse},
		{name: "valid true status with pass", result: monitor.CheckResult{Pass: true, Status: metav1.ConditionTrue}, expected: metav1.ConditionTrue},
		{name: "valid false status without pass", result: monitor.CheckResult{Status: metav1.ConditionFalse}, expected: metav1.ConditionFalse},
		{name: "unknown status", result: monitor.CheckResult{Status: metav1.ConditionUnknown}, expected: metav1.ConditionUnknown},
		{name: "true status contradicting failure is unknown", result: monitor.CheckResult{Status: metav1.ConditionTrue}, expected: metav1.ConditionUnknown},
		{name: "false status contradicting pass is unknown", result: monitor.CheckResult{Pass: true, Status: metav1.ConditionFalse}, expected: metav1.ConditionUnknown},
		{name: "invalid status is unknown", result: monitor.CheckResult{Pass: true, Status: "invalid"}, expected: metav1.ConditionUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			NewWithT(t).Expect(tt.result.ConditionStatus()).To(Equal(tt.expected))
		})
	}
}
