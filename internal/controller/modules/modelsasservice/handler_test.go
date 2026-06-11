package modelsasservice

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func TestNewHandler(t *testing.T) {
	h := NewHandler()
	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}
	if h.Config.Name != moduleName {
		t.Errorf("expected name %q, got %q", moduleName, h.Config.Name)
	}
	if h.Config.CRName != crName {
		t.Errorf("expected CRName %q, got %q", crName, h.Config.CRName)
	}
	if h.Config.GVK != gvk.ModelsAsService {
		t.Errorf("expected GVK %v, got %v", gvk.ModelsAsService, h.Config.GVK)
	}
	expectedImages := 4
	if len(h.Config.RelatedImages) != expectedImages {
		t.Errorf("expected %d related images, got %d", expectedImages, len(h.Config.RelatedImages))
	}
}

func TestIsEnabled(t *testing.T) {
	h := NewHandler()

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
	h := NewHandler()
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
				if u.GetName() != crName {
					t.Errorf("CR name = %q, want %q", u.GetName(), crName)
				}
				if u.GroupVersionKind() != gvk.ModelsAsService {
					t.Errorf("GVK = %v, want %v", u.GroupVersionKind(), gvk.ModelsAsService)
				}
			}
		})
	}
}
