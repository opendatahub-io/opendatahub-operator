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

// TestDSCProjection verifies that DSC Components.AIGateway.ModelsAsService config
// is correctly projected onto the ModelsAsService CR (3.6+ aigateway location).
func TestDSCProjection(t *testing.T) {
	h := modelsasservice.NewHandler()
	ctx := context.Background()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				// 3.6+ location: nested under AIGateway
				AIGateway: componentApi.DSCAIGateway{
					AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
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
		t.Fatal("IsEnabled() = false, want true when MaaS is Managed")
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

	// Step 4: Verify spec is empty (matches component NewCRObject behavior)
	spec, found, err := unstructured.NestedFieldNoCopy(cr.Object, "spec")
	if err != nil || !found {
		t.Fatalf("spec not found in CR: found=%v, err=%v", found, err)
	}

	specMap, ok := spec.(map[string]interface{})
	if !ok {
		t.Fatalf("spec is not a map: %T", spec)
	}

	// Spec should be empty to match component handler's NewCRObject
	if len(specMap) != 0 {
		t.Errorf("spec should be empty, got %d fields: %+v", len(specMap), specMap)
	}

	t.Logf("✅ DSC projection test passed: %s CR created with empty spec (matches component behavior)",
		cr.GetName())
}

// TestDisabledScenarios verifies that MaaS is correctly disabled
// when the MaaS management state is not Managed (3.6+ aigateway behavior).
func TestDisabledScenarios(t *testing.T) {
	h := modelsasservice.NewHandler()

	tests := []struct {
		name          string
		maasState     operatorv1.ManagementState
		expectEnabled bool
	}{
		{
			name:          "MaaS Managed (enabled)",
			maasState:     operatorv1.Managed,
			expectEnabled: true,
		},
		{
			name:          "MaaS Removed (disabled)",
			maasState:     operatorv1.Removed,
			expectEnabled: false,
		},
		{
			name:          "MaaS Unmanaged (disabled)",
			maasState:     operatorv1.Unmanaged,
			expectEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsc := &dscv2.DataScienceCluster{
				Spec: dscv2.DataScienceClusterSpec{
					Components: dscv2.Components{
						// 3.6+ location: nested under AIGateway
						// Kserve state is irrelevant
						AIGateway: componentApi.DSCAIGateway{
							AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
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
				t.Errorf("IsEnabled() = %v, want %v (MaaS=%s)",
					enabled, tt.expectEnabled, tt.maasState)
			}
		})
	}
}

// TestBackwardCompatScenarios verifies that the old 3.4 nested location
// (Kserve.ModelsAsService) still works and respects both Kserve and MaaS states.
func TestBackwardCompatScenarios(t *testing.T) {
	h := modelsasservice.NewHandler()

	tests := []struct {
		name          string
		kserveState   operatorv1.ManagementState
		maasState     operatorv1.ManagementState
		expectEnabled bool
	}{
		{
			name:          "Both Managed (enabled)",
			kserveState:   operatorv1.Managed,
			maasState:     operatorv1.Managed,
			expectEnabled: true,
		},
		{
			name:          "KServe Removed (disabled in 3.4 compat mode)",
			kserveState:   operatorv1.Removed,
			maasState:     operatorv1.Managed,
			expectEnabled: false,
		},
		{
			name:          "MaaS Removed (disabled)",
			kserveState:   operatorv1.Managed,
			maasState:     operatorv1.Removed,
			expectEnabled: false,
		},
		{
			name:          "KServe Unmanaged (disabled in 3.4 compat mode)",
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
						// OLD 3.4 location: nested under Kserve
						// MaaS requires Kserve to be Managed in this mode
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

// TestNewLocationPrecedence verifies that when both locations are set,
// the new 3.6+ aigateway location takes precedence.
func TestNewLocationPrecedence(t *testing.T) {
	h := modelsasservice.NewHandler()

	tests := []struct {
		name             string
		newLocationState operatorv1.ManagementState
		kserveState      operatorv1.ManagementState
		oldLocationState operatorv1.ManagementState
		expectEnabled    bool
		description      string
	}{
		{
			name:             "New Managed, Old Removed (new wins)",
			newLocationState: operatorv1.Managed,
			kserveState:      operatorv1.Managed,
			oldLocationState: operatorv1.Removed,
			expectEnabled:    true,
			description:      "new location takes precedence",
		},
		{
			name:             "New Removed, Old Managed (new wins)",
			newLocationState: operatorv1.Removed,
			kserveState:      operatorv1.Managed,
			oldLocationState: operatorv1.Managed,
			expectEnabled:    false,
			description:      "new location takes precedence",
		},
		{
			name:             "Both Managed (enabled)",
			newLocationState: operatorv1.Managed,
			kserveState:      operatorv1.Managed,
			oldLocationState: operatorv1.Managed,
			expectEnabled:    true,
			description:      "both agree on Managed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsc := &dscv2.DataScienceCluster{
				Spec: dscv2.DataScienceClusterSpec{
					Components: dscv2.Components{
						// NEW 3.6+ location: nested under AIGateway
						AIGateway: componentApi.DSCAIGateway{
							AIGatewayCommonSpec: componentApi.AIGatewayCommonSpec{
								ModelsAsService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: tt.newLocationState,
								},
							},
						},
						// OLD 3.4 location
						Kserve: componentApi.DSCKserve{
							ManagementSpec: common.ManagementSpec{
								ManagementState: tt.kserveState,
							},
							KserveCommonSpec: componentApi.KserveCommonSpec{
								ModelsAsService: componentApi.DSCModelsAsServiceSpec{
									ManagementState: tt.oldLocationState,
								},
							},
						},
					},
				},
			}

			platform := &modules.PlatformContext{DSC: dsc}
			enabled := h.IsEnabled(platform)

			if enabled != tt.expectEnabled {
				t.Errorf("IsEnabled() = %v, want %v (%s): new=%s, old=%s, kserve=%s",
					enabled, tt.expectEnabled, tt.description,
					tt.newLocationState, tt.oldLocationState, tt.kserveState)
			}
		})
	}
}
