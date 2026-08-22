package modules_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	aigatewayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	dashboardModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	feastModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/feastoperator"
	kserveModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/kserve"
	mcplifecycleoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mcplifecycleoperator"
	mlflowoperatorModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/mlflowoperator"
	ogxModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/ogx"
	workbenchesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// allHandlers returns every module handler the platform operator registers.
// Keep in sync with existingModules in cmd/main.go. Monitoring is excluded
// because it is currently commented out in registration.
func allHandlers() []modules.ModuleHandler {
	return []modules.ModuleHandler{
		aigatewayModule.NewHandler(),
		dashboardModule.NewHandler(),
		feastModule.NewHandler(),
		kserveModule.NewHandler(),
		mcplifecycleoperatorModule.NewHandler(),
		mlflowoperatorModule.NewHandler(),
		ogxModule.NewHandler(),
		workbenchesModule.NewHandler(),
	}
}

func managedDSCPlatformContext() *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		Release:               common.Release{Name: "Open Data Hub"},
		ManifestsBasePath:     "/opt/manifests",
		ChartsBasePath:        "/opt/charts",
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Dashboard: componentApi.DSCDashboard{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
					AIGateway: componentApi.DSCAIGateway{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
					Kserve: componentApi.DSCKserve{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
					Workbenches: componentApi.DSCWorkbenches{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
					FeastOperator: componentApi.DSCFeastOperator{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
					MLflowOperator: componentApi.DSCMLflowOperator{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
					MCPLifecycleOperator: componentApi.DSCMCPLifecycleOperator{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
					OGX: componentApi.DSCOGX{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
	}
}

func TestHandlerCompliance_GetNameIsNonEmpty(t *testing.T) {
	seen := make(map[string]bool)
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)
			name := h.GetName()
			g.Expect(name).ShouldNot(BeEmpty(), "GetName must return a non-empty string")
			g.Expect(seen).ShouldNot(HaveKey(name), "duplicate handler name %q", name)
			seen[name] = true
		})
	}
}

func TestHandlerCompliance_GetGVK(t *testing.T) {
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)
			gvk := h.GetGVK()
			g.Expect(gvk.Group).ShouldNot(BeEmpty(), "GVK.Group must be set")
			g.Expect(gvk.Version).ShouldNot(BeEmpty(), "GVK.Version must be set")
			g.Expect(gvk.Kind).ShouldNot(BeEmpty(), "GVK.Kind must be set")
		})
	}
}

func TestHandlerCompliance_GetOperatorManifestsReturnsAtLeastOneSource(t *testing.T) {
	platform := managedDSCPlatformContext()
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)
			manifests := h.GetOperatorManifests(platform)
			g.Expect(len(manifests.HelmCharts)+len(manifests.Manifests)).Should(
				BeNumerically(">", 0),
				"handler must return at least one Helm chart or Kustomize manifest")
		})
	}
}

func TestHandlerCompliance_BuildModuleCR_ValidGVK(t *testing.T) {
	platform := managedDSCPlatformContext()

	cli, err := fakeclient.New()
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)
			cr, err := h.BuildModuleCR(context.Background(), cli, platform)
			g.Expect(err).ShouldNot(HaveOccurred())
			if cr == nil {
				return
			}
			g.Expect(cr.GetKind()).Should(Equal(h.GetGVK().Kind),
				"CR Kind must match handler GVK")
			g.Expect(cr.GroupVersionKind()).Should(Equal(h.GetGVK()),
				"CR GroupVersionKind must match handler GVK")
			g.Expect(cr.GetName()).ShouldNot(BeEmpty(),
				"CR must have a non-empty name")
		})
	}
}

func TestHandlerCompliance_BuildModuleCR_NilPlatformReturnsError(t *testing.T) {
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)
			cr, err := h.BuildModuleCR(context.Background(), nil, nil)
			if err == nil && cr == nil {
				t.Skipf("handler %s returns (nil, nil) for nil platform — acceptable if externally managed", h.GetName())
			}
			g.Expect(err).Should(HaveOccurred(),
				"BuildModuleCR should return an error when PlatformContext is nil")
		})
	}
}

func TestHandlerCompliance_IsEnabledFalseForNilPlatform(t *testing.T) {
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(h.IsEnabled(nil)).Should(BeFalse(),
				"IsEnabled(nil) must return false")
		})
	}
}

func TestHandlerCompliance_IsEnabledFalseForEmptyPlatform(t *testing.T) {
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)
			empty := &modules.PlatformContext{ApplicationsNamespace: "ns"}
			g.Expect(h.IsEnabled(empty)).Should(BeFalse(),
				"IsEnabled should return false when neither DSC nor Platform CR is set")
		})
	}
}

func TestHandlerCompliance_WriteDSCComponentStatusDoesNotPanicOnNilDSC(t *testing.T) {
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			NewWithT(t)
			h.WriteDSCComponentStatus(nil, true, nil)
			h.WriteDSCComponentStatus(nil, false, nil)
		})
	}
}

func TestHandlerCompliance_OptionalInterfaces(t *testing.T) {
	for _, h := range allHandlers() {
		t.Run(h.GetName(), func(t *testing.T) {
			g := NewWithT(t)

			if rct, ok := h.(modules.ReadyConditionTyper); ok {
				g.Expect(rct.GetReadyConditionType()).ShouldNot(BeEmpty(),
					"ReadyConditionTyper.GetReadyConditionType must return non-empty")
			}

			if cn, ok := h.(modules.ContainerNamer); ok {
				g.Expect(cn.GetContainerName()).ShouldNot(BeEmpty(),
					"ContainerNamer.GetContainerName must return non-empty")
			}

			if dn, ok := h.(modules.DeploymentNamer); ok {
				name := dn.GetDeploymentName()
				_ = name // may be empty — handler falls back to release name
			}

			if scp, ok := h.(modules.SubmoduleConditionProvider); ok {
				for _, sm := range scp.GetSubmoduleConditions() {
					g.Expect(sm.SourceConditionType).ShouldNot(BeEmpty(),
						"SubmoduleCondition.SourceConditionType must be set")
					g.Expect(sm.DSCConditionType).ShouldNot(BeEmpty(),
						"SubmoduleCondition.DSCConditionType must be set")
				}
			}
		})
	}
}
