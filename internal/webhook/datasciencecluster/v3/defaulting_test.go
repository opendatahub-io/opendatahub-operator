package v3_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"

	. "github.com/onsi/gomega"
)

// ptrManagementState returns a pointer to the given ManagementState.
func ptrManagementState(ms operatorv1.ManagementState) *operatorv1.ManagementState {
	return new(ms)
}

// TestDefaulterV3_DefaultingLogic exercises the defaulting webhook logic for DataScienceCluster v3 resources.
func TestDefaulterV3_DefaultingLogic(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	testCases := []struct {
		name                 string
		managementState      *operatorv1.ManagementState // pointer: nil means AI Hub not set at all
		applicationNamespace *string                     // pointer: nil means not set
		expectedNamespace    string
	}{
		{
			name:                 "Sets default ApplicationNamespace if empty and Managed",
			managementState:      new(operatorv1.Managed),
			applicationNamespace: new(""),
			expectedNamespace:    componentApi.DefaultModelRegistriesNamespace,
		},
		{
			name:                 "Does not overwrite custom ApplicationNamespace if set",
			managementState:      new(operatorv1.Managed),
			applicationNamespace: new("custom-ns"),
			expectedNamespace:    "custom-ns",
		},
		{
			name:                 "Does nothing if not Managed",
			managementState:      new(operatorv1.Removed),
			applicationNamespace: new(""),
			expectedNamespace:    "",
		},
		{
			name:                 "Does nothing if AI Hub is not set at all (upgrade case)",
			managementState:      nil, // AI Hub not set at all
			applicationNamespace: nil, // not set
			expectedNamespace:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsc := &dscv3.DataScienceCluster{}
			if tc.managementState != nil || tc.applicationNamespace != nil {
				// Only set AI Hub if at least one field is set
				if tc.managementState != nil {
					dsc.Spec.Components.AIHub.ManagementState = *tc.managementState
				}
				if tc.applicationNamespace != nil {
					dsc.Spec.Components.AIHub.ApplicationNamespace = *tc.applicationNamespace
				}
			}

			defaulter := &v3webhook.Defaulter{Name: "test-v3"}
			err := defaulter.Default(ctx, dsc)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(dsc.Spec.Components.AIHub.ApplicationNamespace).To(Equal(tc.expectedNamespace))
		})
	}
}

// TestDefaulterV3_NIMDefaultingLogic exercises the NIM defaulting webhook logic for DataScienceCluster v3 resources.
func TestDefaulterV3_NIMDefaultingLogic(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	testCases := []struct {
		name                  string
		kserveManagementState *operatorv1.ManagementState
		nimManagementState    *operatorv1.ManagementState
		expectedNIMState      operatorv1.ManagementState
	}{
		{
			name:                  "Sets default NIM ManagementState if empty and Kserve is Managed",
			kserveManagementState: new(operatorv1.Managed),
			nimManagementState:    ptrManagementState(""),
			expectedNIMState:      operatorv1.Managed,
		},
		{
			name:                  "Does not overwrite NIM ManagementState if already set",
			kserveManagementState: new(operatorv1.Managed),
			nimManagementState:    new(operatorv1.Removed),
			expectedNIMState:      operatorv1.Removed,
		},
		{
			name:                  "Does nothing if Kserve is not Managed",
			kserveManagementState: new(operatorv1.Removed),
			nimManagementState:    ptrManagementState(""),
			expectedNIMState:      "",
		},
		{
			name:                  "Does nothing if Kserve is not set at all (upgrade case)",
			kserveManagementState: nil,
			nimManagementState:    nil,
			expectedNIMState:      "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsc := &dscv3.DataScienceCluster{}
			if tc.kserveManagementState != nil {
				dsc.Spec.Components.Kserve.ManagementState = *tc.kserveManagementState
			}
			if tc.nimManagementState != nil {
				dsc.Spec.Components.Kserve.NIM.ManagementState = *tc.nimManagementState
			}

			defaulter := &v3webhook.Defaulter{Name: "test-v3"}
			err := defaulter.Default(ctx, dsc)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(dsc.Spec.Components.Kserve.NIM.ManagementState).To(Equal(tc.expectedNIMState))
		})
	}
}
