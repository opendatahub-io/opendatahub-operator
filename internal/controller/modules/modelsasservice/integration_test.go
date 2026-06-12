package modelsasservice_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/modelsasservice"
)

// TestDSCProjection verifies that DSC Kserve.ModelsAsService config
// is correctly projected onto the ModelsAsService CR.
func TestDSCProjection(t *testing.T) {
	h := modelsasservice.NewHandler()
	ctx := context.Background()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
		},
	}

	platform := &modules.PlatformContext{
		DSC: dsc,
	}

	// Step 1: IsEnabled should return true
	if !h.IsEnabled(platform) {
		t.Fatal("IsEnabled() = false, want true when both KServe and MaaS are Managed")
	}

	// Step 2: BuildModuleCR should create valid CR
	cr, err := h.BuildModuleCR(ctx, nil, platform)
	if err != nil {
		t.Fatalf("BuildModuleCR() error = %v", err)
	}

	if cr == nil {
		t.Fatal("BuildModuleCR() returned nil CR")
	}

	// Step 3: Verify CR structure
	if cr.GetName() != componentApi.ModelsAsServiceInstanceName {
		t.Errorf("CR name = %q, want %q", cr.GetName(), componentApi.ModelsAsServiceInstanceName)
	}

	// Step 4: Verify spec contains managementState
	spec, found, err := unstructured.NestedFieldNoCopy(cr.Object, "spec")
	if err != nil || !found {
		t.Fatalf("spec not found in CR: found=%v, err=%v", found, err)
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		t.Fatalf("spec is not a map: %T", spec)
	}

	mgmtState, found := specMap["managementState"]
	if !found {
		t.Error("managementState not found in spec")
	}

	if mgmtState != string(operatorv1.Managed) {
		t.Errorf("managementState = %v, want %v", mgmtState, string(operatorv1.Managed))
	}

	t.Logf("✅ DSC projection test passed: %s CR created with managementState=%s",
		cr.GetName(), mgmtState)
}

// TestDisabledScenarios verifies that MaaS is correctly disabled
// when KServe or MaaS management states are not Managed.
func TestDisabledScenarios(t *testing.T) {
	h := modelsasservice.NewHandler()

	tests := []struct {
		name          string
		kserveState   operatorv1.ManagementState
		maasState     operatorv1.ManagementState
		expectEnabled bool
	}{
		{
			name:          "KServe Removed",
			kserveState:   operatorv1.Removed,
			maasState:     operatorv1.Managed,
			expectEnabled: false,
		},
		{
			name:          "MaaS Removed",
			kserveState:   operatorv1.Managed,
			maasState:     operatorv1.Removed,
			expectEnabled: false,
		},
		{
			name:          "Both Managed",
			kserveState:   operatorv1.Managed,
			maasState:     operatorv1.Managed,
			expectEnabled: true,
		},
		{
			name:          "KServe Unmanaged",
			kserveState:   operatorv1.Unmanaged,
			maasState:     operatorv1.Managed,
			expectEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsc := &dscv2.DataScienceCluster{
				Spec: dscv2.DataScienceClusterSpec{
					Components: dscv2.Components{
						Kserve: componentApi.DSCKserve{
							ManagementSpec: common.ManagementSpec{
								ManagementState: tt.kserveState,
							},
							KserveCommonSpec: componentApi.KserveCommonSpec{
								ModelsAsService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: tt.maasState,
								},
							},
						},
					},
				},
			}

			platform := &modules.PlatformContext{DSC: dsc}
			enabled := h.IsEnabled(platform)

			if enabled != tt.expectEnabled {
				t.Errorf("IsEnabled() = %v, want %v (KServe=%s, MaaS=%s)",
					enabled, tt.expectEnabled, tt.kserveState, tt.maasState)
			}
		})
	}
}
