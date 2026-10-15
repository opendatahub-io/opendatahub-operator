package aihub_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aihub"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	testconditions "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestComputeAIHubStatus(t *testing.T) {
	h := aihub.NewHandler()
	modules.DefaultRegistry().Add(h)

	for _, tc := range []struct {
		name       string
		state      operatorv1.ManagementState
		ready      metav1.ConditionStatus
		observed   int64
		wantReady  metav1.ConditionStatus
		wantReason string
	}{
		{"ready", operatorv1.Managed, metav1.ConditionTrue, 2, metav1.ConditionTrue, "Available"},
		{"not ready", operatorv1.Managed, metav1.ConditionFalse, 2, metav1.ConditionFalse, "Available"},
		{"stale generation", operatorv1.Managed, metav1.ConditionTrue, 1, metav1.ConditionFalse, status.NotReadyReason},
		{"removed", operatorv1.Removed, metav1.ConditionTrue, 2, metav1.ConditionFalse, status.RemovedReason},
		{"empty state", "", metav1.ConditionTrue, 2, metav1.ConditionFalse, status.RemovedReason},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			dscCtx := newDSCCtx(tc.state)
			dsc := dscCtx.DSC
			// Disabled components must clear status left by a previous deployment.
			dsc.Status.Components.AIHub.AIHubCommonStatus = &componentApi.AIHubCommonStatus{
				InstancesNamespace: "old-registries",
				ComponentReleaseStatus: common.ComponentReleaseStatus{
					Releases: []common.ComponentRelease{{Name: "old-release"}},
				},
			}
			moduleCR, err := h.BuildModuleCR(t.Context(), nil, dscCtx, &modules.ModuleCRConfig{
				ApplicationsNamespace: "platform-apps",
			})
			g.Expect(err).To(Succeed())
			moduleCR.SetGeneration(2)
			moduleCR.Object["status"] = map[string]any{
				"observedGeneration": tc.observed,
				"conditions": []any{map[string]any{
					"type": "Ready", "status": string(tc.ready),
					"reason": "Available", "message": "child operators checked",
				}},
				"releases": []any{map[string]any{"name": "model-registry", "version": "1.0.0"}},
			}
			cli, err := fakeclient.New(
				fakeclient.WithObjects(moduleCR),
				fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: gvk.AIHub, Scope: meta.RESTScopeRoot}),
			)
			g.Expect(err).To(Succeed())
			rr := &types.ReconciliationRequest{
				Client: cli, Instance: dsc,
				Conditions: testconditions.NewManager(dsc, status.ConditionTypeModulesReady),
			}
			g.Expect(modules.ComputeModulesStatusDetailed(t.Context(), rr)).To(Succeed())

			ready := conditions.FindStatusCondition(dsc.GetStatus(), "AIHubReady")
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(tc.wantReady))
			g.Expect(ready.Reason).To(Equal(tc.wantReason))
			if tc.observed == 2 && tc.state == operatorv1.Managed {
				g.Expect(ready.Message).To(Equal("child operators checked"))
				g.Expect(dsc.Status.Components.AIHub.Releases).To(Equal(
					[]common.ComponentRelease{{Name: "model-registry", Version: "1.0.0"}},
				))
			}
			if tc.state == operatorv1.Managed {
				g.Expect(dsc.Status.Components.AIHub.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).To(Equal(componentApi.DefaultModelRegistriesNamespace))
			} else {
				g.Expect(dsc.Status.Components.AIHub.ManagementState).To(Equal(operatorv1.Removed))
				g.Expect(dsc.Status.Components.AIHub.InstancesNamespace).To(BeEmpty())
				g.Expect(dsc.Status.Components.AIHub.Releases).To(BeEmpty())
			}

			legacy := &dscv2.DataScienceCluster{}
			g.Expect(legacy.ConvertFrom(dsc)).To(Succeed())
			g.Expect(legacy.Status.Components.ModelRegistry.ManagementState).To(Equal(dsc.Status.Components.AIHub.ManagementState))
			g.Expect(legacy.Status.Components.ModelRegistry.RegistriesNamespace).To(Equal(dsc.Status.Components.AIHub.InstancesNamespace))
			g.Expect(legacy.Status.Components.ModelRegistry.Releases).To(Equal(dsc.Status.Components.AIHub.Releases))
			legacyReady := conditions.FindStatusCondition(legacy.GetStatus(), "ModelRegistryReady")
			g.Expect(legacyReady).NotTo(BeNil())
			wantLegacyReady := *ready
			wantLegacyReady.Type = "ModelRegistryReady"
			g.Expect(*legacyReady).To(Equal(wantLegacyReady))
			g.Expect(conditions.FindStatusCondition(legacy.GetStatus(), "AIHubReady")).To(BeNil())
		})
	}
}
