//nolint:testpackage // Verifies handler internals such as defaults and projected fields.
package workbenches

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

func newPlatformModules(mgmtState operatorv1.ManagementState) *configApi.PlatformModules {
	return &configApi.PlatformModules{
		Workbenches: common.ManagementSpec{
			ManagementState: mgmtState,
		},
	}
}

func newDSCCtx(mgmtState operatorv1.ManagementState) *modules.DSCContext {
	return &modules.DSCContext{
		DSC: &dscApi.DataScienceCluster{
			Spec: dscApi.DataScienceClusterSpec{
				Components: dscApi.Components{
					Workbenches: componentApi.DSCWorkbenches{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
						WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
							WorkbenchNamespace: "my-workbenches",
							WorkbenchesV2: componentApi.WorkbenchesV2Spec{
								ManagementState: operatorv1.Removed,
							},
						},
					},
					MLflowOperator: componentApi.DSCMLflowOperator{
						ManagementSpec: common.ManagementSpec{
							ManagementState: operatorv1.Removed,
						},
					},
				},
			},
		},
	}
}

func newModuleCRConfig() *modules.ModuleCRConfig {
	return &modules.ModuleCRConfig{
		GatewayDomain:    "apps.example.com",
		GatewayIngresses: []serviceApi.AdditionalIngressStatus{},
		Release: common.Release{
			Name: cluster.OpenDataHub,
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_NilModules(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Managed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformModules(operatorv1.Managed))).Should(BeTrue())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()

	u, err := h.BuildModuleCR(context.Background(), nil, newDSCCtx(operatorv1.Managed), newModuleCRConfig())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.WorkbenchesInstanceName))
	g.Expect(u.GetKind()).Should(Equal(componentApi.WorkbenchesKind))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["workbenchNamespace"]).Should(Equal("my-workbenches"))
	g.Expect(spec["managementState"]).Should(Equal("Managed"))
	g.Expect(spec["gatewayDomain"]).Should(Equal("apps.example.com"))
	g.Expect(spec["platform"]).Should(Equal("OpenDataHub"))
	g.Expect(spec["mlflowEnabled"]).Should(BeFalse())
}

func TestBuildModuleCR_ProjectsMLflowEnabled(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	dscCtx := newDSCCtx(operatorv1.Managed)
	dscCtx.DSC.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	u, err := h.BuildModuleCR(context.Background(), nil, dscCtx, newModuleCRConfig())
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["mlflowEnabled"]).Should(BeTrue())
}

func TestBuildModuleCR_ProjectsIngresses(t *testing.T) {
	g := NewWithT(t)
	previousCluster := cluster.GetClusterInfo()
	t.Cleanup(func() { cluster.SetClusterInfo(previousCluster) })
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	cfg := newModuleCRConfig()
	cfg.GatewayIngresses = []serviceApi.AdditionalIngressStatus{
		{Name: "beta", Hostname: "beta.example.com", GatewayRef: serviceApi.GatewayReference{Name: "beta-parent", Namespace: gateway.GatewayNamespace}},
		{Name: "alpha", Hostname: "alpha.example.com", GatewayRef: serviceApi.GatewayReference{Name: "alpha", Namespace: gateway.GatewayNamespace}},
		{Name: "pending", Hostname: "pending.example.com"},
	}
	build := func() (*unstructured.Unstructured, error) {
		return NewHandler().BuildModuleCR(context.Background(), nil, newDSCCtx(operatorv1.Managed), cfg)
	}
	u, err := build()
	g.Expect(err).NotTo(HaveOccurred())
	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(spec).NotTo(HaveKey("additionalIngressNames"))
	g.Expect(spec["ingresses"]).To(Equal([]map[string]any{
		{"name": gateway.DefaultGatewayName, "gatewayName": gateway.DefaultGatewayName, "gatewayNamespace": gateway.GatewayNamespace, "hostname": "apps.example.com", "isDefault": true},
		{"name": "beta", "gatewayName": "beta-parent", "gatewayNamespace": gateway.GatewayNamespace, "hostname": "beta.example.com"},
		{"name": "alpha", "gatewayName": "alpha", "gatewayNamespace": gateway.GatewayNamespace, "hostname": "alpha.example.com"},
	}))
	validateModuleCRAgainstBundledSchema(t, u.Object)

	cfg.GatewayIngresses = []serviceApi.AdditionalIngressStatus{}
	u, err = build()
	g.Expect(err).NotTo(HaveOccurred())
	spec, ok = u.Object["spec"].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(spec["ingresses"]).To(Equal([]map[string]any{
		{"name": gateway.DefaultGatewayName, "gatewayName": gateway.DefaultGatewayName, "gatewayNamespace": gateway.GatewayNamespace, "hostname": "apps.example.com", "isDefault": true},
	}))

	cfg.GatewayIngresses = nil
	u, err = build()
	g.Expect(err).NotTo(HaveOccurred())
	spec, ok = u.Object["spec"].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(spec["ingresses"]).To(Equal([]map[string]any{}))

	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeKubernetes})
	cfg.GatewayIngresses = []serviceApi.AdditionalIngressStatus{}
	u, err = build()
	g.Expect(err).NotTo(HaveOccurred())
	spec, ok = u.Object["spec"].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(spec["ingresses"]).To(Equal([]map[string]any{
		{
			"name": gateway.XKSDefaultGatewayName, "gatewayName": gateway.XKSDefaultGatewayName,
			"gatewayNamespace": gateway.XKSGatewayNamespace, "hostname": "apps.example.com", "isDefault": true,
		},
	}))
}

func TestBuildModuleCR_ProjectsWorkbenchesV2Submodule(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()

	u, err := h.BuildModuleCR(context.Background(), nil, newDSCCtx(operatorv1.Managed), newModuleCRConfig())
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")

	_, hasV1 := spec["notebooksV1"]
	g.Expect(hasV1).Should(BeFalse(), "notebooksV1 must not be projected")

	nbV2, ok := spec["workbenchesV2"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "workbenchesV2 is not a map")
	g.Expect(nbV2["managementState"]).Should(Equal("Removed"))
}

func TestSubmoduleIsEnabled_WorkbenchesV2(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	subs := h.GetSubmoduleConditions()
	g.Expect(subs).Should(HaveLen(1))

	v2Enabled := subs[0].IsEnabled
	g.Expect(v2Enabled(newDSCCtx(operatorv1.Managed))).Should(BeFalse(),
		"workbenchesV2 defaults to Removed in test fixture")
	g.Expect(v2Enabled(nil)).Should(BeFalse())

	managedCtx := newDSCCtx(operatorv1.Managed)
	managedCtx.DSC.Spec.Components.Workbenches.WorkbenchesV2.ManagementState = operatorv1.Managed
	g.Expect(v2Enabled(managedCtx)).Should(BeTrue())
}

func TestBuildModuleCR_PlatformMode(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()

	u, err := h.BuildModuleCR(context.Background(), nil, newDSCCtx(operatorv1.Managed), newModuleCRConfig())
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(u.GetName()).Should(Equal(componentApi.WorkbenchesInstanceName))

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["managementState"]).Should(Equal("Managed"))
	g.Expect(spec["gatewayDomain"]).Should(Equal("apps.example.com"))
	g.Expect(spec["platform"]).Should(Equal("OpenDataHub"))
}

func TestBuildModuleCR_NilPlatformContextReturnsError(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	_, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	g.Expect(err).Should(HaveOccurred())
}

func TestGetDeploymentName(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.GetDeploymentName()).Should(Equal(ControllerDeploymentName))
}

func TestImageHandling(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()

	g.Expect(h.GetControllerImage()).Should(Equal("RELATED_IMAGE_ODH_WORKBENCHES_OPERATOR_IMAGE"))
	g.Expect(h.GetRelatedImages()).Should(ContainElement("RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE"))
	g.Expect(h.GetRelatedImages()).Should(ContainElement("RELATED_IMAGE_ODH_WORKBENCHES_CONTROLLER_IMAGE"))
	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_WORKBENCHES_OPERATOR_IMAGE"))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.WorkbenchesComponentName))
}

func TestWriteLegacyStatusFields_MirrorsFromDSCSpec(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: "rhods-notebooks",
					},
				},
			},
		},
	}

	err := h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(Equal("rhods-notebooks"))
}

func TestWriteLegacyStatusFields_ClearsWhenDisabled(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: "rhods-notebooks",
					},
				},
			},
		},
	}

	err := h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())

	err = h.WriteLegacyStatusFields(context.Background(), nil, dsc, false)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(BeEmpty())
}

func TestWriteLegacyStatusFields_ClearsWhenSpecEmpty(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	dsc := &dscApi.DataScienceCluster{
		Spec: dscApi.DataScienceClusterSpec{
			Components: dscApi.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: "rhods-notebooks",
					},
				},
			},
		},
	}

	err := h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(Equal("rhods-notebooks"))

	dsc.Spec.Components.Workbenches.WorkbenchNamespace = ""
	err = h.WriteLegacyStatusFields(context.Background(), nil, dsc, true)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(dsc.Status.Components.Workbenches.WorkbenchNamespace).Should(BeEmpty())
}

func TestBuildModuleCRMatchesBundledCRDSchema(t *testing.T) {
	t.Parallel()

	h := NewHandler()
	moduleCR, err := h.BuildModuleCR(context.Background(), nil, newDSCCtx(operatorv1.Managed), newModuleCRConfig())
	if err != nil {
		t.Fatalf("build module CR: %v", err)
	}

	validateModuleCRAgainstBundledSchema(t, moduleCR.Object)
}

func TestBuildModuleCRMatchesBundledCRDSchemaPlatformMode(t *testing.T) {
	t.Parallel()

	h := NewHandler()
	moduleCR, err := h.BuildModuleCR(context.Background(), nil, newDSCCtx(operatorv1.Managed), newModuleCRConfig())
	if err != nil {
		t.Fatalf("build module CR in platform mode: %v", err)
	}

	validateModuleCRAgainstBundledSchema(t, moduleCR.Object)
}

func validateModuleCRAgainstBundledSchema(t *testing.T, moduleCR map[string]any) {
	t.Helper()

	schema := loadWorkbenchesCRDSchema(t)
	validator, _, err := apiextensionsvalidation.NewSchemaValidator(schema)
	if err != nil {
		t.Fatalf("build schema validator: %v", err)
	}

	errs := apiextensionsvalidation.ValidateCustomResource(field.NewPath("workbenches"), moduleCR, validator)
	if len(errs) > 0 {
		t.Fatalf("module CR does not match bundled CRD schema: %v", errs.ToAggregate())
	}
}

func loadWorkbenchesCRDSchema(t *testing.T) *apiextensions.JSONSchemaProps {
	t.Helper()

	crdPath := filepath.Join(
		"..", "..", "..", "..",
		"opt", "charts", "workbenches", "crd", "workbenches.crd.yaml",
	)

	data, err := os.ReadFile(crdPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("skipping schema validation test; bundled Workbenches CRD not found at %s (run make get-manifests)", crdPath)
		}
		t.Fatalf("read bundled Workbenches CRD: %v", err)
	}

	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatalf("unmarshal bundled Workbenches CRD: %v", err)
	}

	var versionSchema *apiextensionsv1.CustomResourceValidation
	for i := range crd.Spec.Versions {
		version := &crd.Spec.Versions[i]
		if version.Storage {
			versionSchema = version.Schema
			break
		}
	}
	if versionSchema == nil || versionSchema.OpenAPIV3Schema == nil {
		t.Fatal("missing storage schema in bundled Workbenches CRD")
	}

	schemaBytes, err := json.Marshal(versionSchema.OpenAPIV3Schema)
	if err != nil {
		t.Fatalf("marshal CRD schema: %v", err)
	}

	var internalSchema apiextensions.JSONSchemaProps
	if err := json.Unmarshal(schemaBytes, &internalSchema); err != nil {
		t.Fatalf("convert schema to internal apiextensions form: %v", err)
	}

	return &internalSchema
}
