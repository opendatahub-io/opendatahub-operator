package modelsasservice_test

import (
	"context"
	"testing"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/modelsasservice"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func TestNewHandler(t *testing.T) {
	h := modelsasservice.NewHandler()
	if h == nil {
		t.Fatal("modelsasservice.NewHandler() returned nil")
	}
	if h.Config.Name != modelsasservice.ModuleName {
		t.Errorf("expected name %q, got %q", modelsasservice.ModuleName, h.Config.Name)
	}
	if h.Config.CRName != modelsasservice.CRName {
		t.Errorf("expected CRName %q, got %q", modelsasservice.CRName, h.Config.CRName)
	}
	if h.Config.GVK != gvk.ModelsAsService {
		t.Errorf("expected GVK %v, got %v", gvk.ModelsAsService, h.Config.GVK)
	}
	expectedImages := 4
	if len(h.Config.RelatedImages) != expectedImages {
		t.Errorf("expected %d related images, got %d", expectedImages, len(h.Config.RelatedImages))
	}
	if h.Config.ControllerImage != "RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE" {
		t.Errorf("expected ControllerImage = %q, got %q", "RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE", h.Config.ControllerImage)
	}
}

func TestIsEnabled(t *testing.T) {
	h := modelsasservice.NewHandler()

	tests := []struct {
		name     string
		platform *modules.PlatformContext
		want     bool
	}{
		{
			name:     "nil platform context",
			platform: nil,
			want:     false,
		},
		{
			name: "DSC mode - KServe disabled",
			platform: &modules.PlatformContext{
				DSC: &dscv2.DataScienceCluster{
					Spec: dscv2.DataScienceClusterSpec{
						Components: dscv2.Components{
							Kserve: componentApi.DSCKserve{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Removed,
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "DSC mode - KServe enabled, MaaS disabled",
			platform: &modules.PlatformContext{
				DSC: &dscv2.DataScienceCluster{
					Spec: dscv2.DataScienceClusterSpec{
						Components: dscv2.Components{
							Kserve: componentApi.DSCKserve{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Managed,
								},
								KserveCommonSpec: componentApi.KserveCommonSpec{
									ModelsAsService: componentApi.DSCModelsAsServiceSpec{
										ManagementState: operatorv1.Removed,
									},
								},
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "DSC mode - both enabled",
			platform: &modules.PlatformContext{
				DSC: &dscv2.DataScienceCluster{
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
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.IsEnabled(tt.platform)
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildModuleCR(t *testing.T) {
	h := modelsasservice.NewHandler()
	ctx := context.Background()

	tests := []struct {
		name     string
		platform *modules.PlatformContext
		wantErr  bool
	}{
		{
			name:     "nil platform context",
			platform: nil,
			wantErr:  true,
		},
		{
			name: "DSC mode - valid",
			platform: &modules.PlatformContext{
				DSC: &dscv2.DataScienceCluster{
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
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := h.BuildModuleCR(ctx, nil, tt.platform)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildModuleCR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if u == nil {
					t.Fatal("BuildModuleCR() returned nil unstructured")
				}
				if u.GetName() != modelsasservice.CRName {
					t.Errorf("CR name = %q, want %q", u.GetName(), modelsasservice.CRName)
				}
				if u.GroupVersionKind() != gvk.ModelsAsService {
					t.Errorf("GVK = %v, want %v", u.GroupVersionKind(), gvk.ModelsAsService)
				}
			}
		})
	}
}

func TestGetModuleStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = maasv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name              string
		tenant            *maasv1alpha1.Tenant
		expectError       bool
		expectCondition   string
		expectCondStatus  metav1.ConditionStatus
		expectCondReason  string
		expectGeneration  int64
		expectObservedGen int64
	}{
		{
			name:              "Tenant not found",
			tenant:            nil,
			expectError:       false,
			expectCondition:   status.ConditionTypeReady,
			expectCondStatus:  metav1.ConditionFalse,
			expectCondReason:  status.NotReadyReason,
			expectGeneration:  0,
			expectObservedGen: 0,
		},
		{
			name: "Tenant being deleted",
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:              maasv1alpha1.TenantInstanceName,
					Namespace:         modelsasservice.TenantSubscriptionNamespace,
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{"maas.opendatahub.io/cleanup"},
					Generation:        5,
				},
				Status: maasv1alpha1.TenantStatus{
					Phase: "Active",
					Conditions: []metav1.Condition{
						{
							Type:   status.ConditionTypeReady,
							Status: metav1.ConditionTrue,
							Reason: "TenantReady",
						},
					},
				},
			},
			expectError:       false,
			expectCondition:   status.ConditionTypeReady,
			expectCondStatus:  metav1.ConditionFalse,
			expectCondReason:  status.DeletingReason,
			expectGeneration:  5,
			expectObservedGen: 0,
		},
		{
			name: "Tenant ready",
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:       maasv1alpha1.TenantInstanceName,
					Namespace:  modelsasservice.TenantSubscriptionNamespace,
					Generation: 5,
				},
				Status: maasv1alpha1.TenantStatus{
					Phase: "Active",
					Conditions: []metav1.Condition{
						{
							Type:   status.ConditionTypeReady,
							Status: metav1.ConditionTrue,
							Reason: "TenantReady",
						},
					},
				},
			},
			expectError:       false,
			expectCondition:   status.ConditionTypeReady,
			expectCondStatus:  metav1.ConditionTrue,
			expectCondReason:  "TenantReady",
			expectGeneration:  5,
			expectObservedGen: 0,
		},
		{
			name: "Tenant degraded",
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:       maasv1alpha1.TenantInstanceName,
					Namespace:  modelsasservice.TenantSubscriptionNamespace,
					Generation: 3,
				},
				Status: maasv1alpha1.TenantStatus{
					Phase: "Degraded",
					Conditions: []metav1.Condition{
						{
							Type:   status.ConditionTypeReady,
							Status: metav1.ConditionFalse,
							Reason: "DeploymentFailed",
						},
					},
				},
			},
			expectError:       false,
			expectCondition:   status.ConditionTypeReady,
			expectCondStatus:  metav1.ConditionFalse,
			expectCondReason:  "DeploymentFailed",
			expectGeneration:  3,
			expectObservedGen: 0,
		},
		{
			name: "Tenant exists but empty conditions",
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:       maasv1alpha1.TenantInstanceName,
					Namespace:  modelsasservice.TenantSubscriptionNamespace,
					Generation: 1,
				},
				Status: maasv1alpha1.TenantStatus{
					Phase:      "Pending",
					Conditions: []metav1.Condition{}, // Empty conditions
				},
			},
			expectError:       false,
			expectGeneration:  1,
			expectObservedGen: 0,
			// No condition assertions - we expect empty conditions
		},
		{
			name: "Tenant with both Ready and Degraded conditions",
			tenant: &maasv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:       maasv1alpha1.TenantInstanceName,
					Namespace:  modelsasservice.TenantSubscriptionNamespace,
					Generation: 7,
				},
				Status: maasv1alpha1.TenantStatus{
					Phase: "Degraded",
					Conditions: []metav1.Condition{
						{
							Type:   status.ConditionTypeReady,
							Status: metav1.ConditionTrue,
							Reason: "TenantReady",
						},
						{
							Type:   status.ConditionTypeDegraded,
							Status: metav1.ConditionTrue,
							Reason: "PartialOutage",
						},
					},
				},
			},
			expectError:       false,
			expectGeneration:  7,
			expectObservedGen: 0,
			// Will verify both conditions exist below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []runtime.Object
			if tt.tenant != nil {
				objs = append(objs, tt.tenant)
			}

			cli := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			handler := modelsasservice.NewHandler()
			moduleStatus, err := handler.GetModuleStatus(context.Background(), cli)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, moduleStatus)

			// Verify generation metadata
			assert.Equal(t, tt.expectGeneration, moduleStatus.Generation, "Generation mismatch")
			assert.Equal(t, tt.expectObservedGen, moduleStatus.ObservedGeneration, "ObservedGeneration mismatch")

			// Verify conditions if expected
			if tt.expectCondition != "" {
				require.NotEmpty(t, moduleStatus.Conditions, "Expected conditions but got none")

				// Find the expected condition
				var found bool
				for _, cond := range moduleStatus.Conditions {
					if cond.Type == tt.expectCondition {
						assert.Equal(t, tt.expectCondStatus, cond.Status, "Condition status mismatch")
						assert.Equal(t, tt.expectCondReason, cond.Reason, "Condition reason mismatch")
						found = true
						break
					}
				}
				assert.True(t, found, "Expected condition %s not found", tt.expectCondition)
			}

			// Special case: verify both Ready and Degraded conditions exist
			if tt.name == "Tenant with both Ready and Degraded conditions" {
				assert.Len(t, moduleStatus.Conditions, 2, "Expected 2 conditions")

				var hasReady, hasDegraded bool
				for _, cond := range moduleStatus.Conditions {
					if cond.Type == status.ConditionTypeReady && cond.Status == metav1.ConditionTrue {
						hasReady = true
					}
					if cond.Type == status.ConditionTypeDegraded && cond.Status == metav1.ConditionTrue {
						hasDegraded = true
					}
				}
				assert.True(t, hasReady, "Expected Ready=True condition")
				assert.True(t, hasDegraded, "Expected Degraded=True condition")
			}

			// Special case: empty conditions
			if tt.name == "Tenant exists but empty conditions" {
				assert.Empty(t, moduleStatus.Conditions, "Expected empty conditions")
			}
		})
	}
}
