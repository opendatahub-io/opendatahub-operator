package v1_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	v1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v1"

	. "github.com/onsi/gomega"
)

// ptrManagementState returns a pointer to the given ManagementState.
func ptrManagementState(ms operatorv1.ManagementState) *operatorv1.ManagementState {
	return &ms
}

// ptrString returns a pointer to the given string.
func ptrString(s string) *string {
	return &s
}

// TestDefaulterV1_DefaultingLogic exercises the defaulting webhook logic for DataScienceCluster v1 resources.
func TestDefaulterV1_DefaultingLogic(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	testCases := []struct {
		name                string
		managementState     *operatorv1.ManagementState // pointer: nil means ModelRegistry not set at all
		registriesNamespace *string                     // pointer: nil means not set
		expectedNamespace   string
	}{
		{
			name:                "Sets default RegistriesNamespace if empty and Managed",
			managementState:     ptrManagementState(operatorv1.Managed),
			registriesNamespace: ptrString(""),
			expectedNamespace:   modelregistryctrl.DefaultModelRegistriesNamespace,
		},
		{
			name:                "Does not overwrite custom RegistriesNamespace if set",
			managementState:     ptrManagementState(operatorv1.Managed),
			registriesNamespace: ptrString("custom-ns"),
			expectedNamespace:   "custom-ns",
		},
		{
			name:                "Does nothing if not Managed",
			managementState:     ptrManagementState(operatorv1.Removed),
			registriesNamespace: ptrString(""),
			expectedNamespace:   "",
		},
		{
			name:                "Does nothing if ModelRegistry is not set at all (upgrade case)",
			managementState:     nil, // ModelRegistry not set at all
			registriesNamespace: nil, // not set
			expectedNamespace:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsc := &dscv1.DataScienceCluster{}
			if tc.managementState != nil || tc.registriesNamespace != nil {
				// Only set ModelRegistry if at least one field is set
				if tc.managementState != nil {
					dsc.Spec.Components.ModelRegistry.ManagementState = *tc.managementState
				}
				if tc.registriesNamespace != nil {
					dsc.Spec.Components.ModelRegistry.RegistriesNamespace = *tc.registriesNamespace
				}
			}

			defaulter := &v1webhook.Defaulter{Name: "test-v1"}
			err := defaulter.Default(ctx, dsc)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(dsc.Spec.Components.ModelRegistry.RegistriesNamespace).To(Equal(tc.expectedNamespace))
		})
	}
}
