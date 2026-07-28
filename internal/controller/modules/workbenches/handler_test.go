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
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

func newPlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		GatewayDomain:         "apps.example.com",
		Release: common.Release{
			Name: cluster.OpenDataHub,
		},
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Workbenches: componentApi.DSCWorkbenches{
						ManagementSpec: common.ManagementSpec{
							ManagementState: mgmtState,
						},
						WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
							WorkbenchNamespace: "my-workbenches",
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

func newPlatformModePlatformCtx(mgmtState operatorv1.ManagementState) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "opendatahub",
		GatewayDomain:         "apps.example.com",
		Release: common.Release{
			Name: cluster.OpenDataHub,
		},
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					Workbenches: common.ManagementSpec{
						ManagementState: mgmtState,
					},
				},
			},
		},
	}
}

func TestIsEnabled_Managed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestIsEnabled_Removed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformCtx(operatorv1.Removed))).Should(BeFalse())
}

func TestIsEnabled_NilPlatformContext(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(nil)).Should(BeFalse())
}

func TestIsEnabled_PlatformMode_Managed(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.IsEnabled(newPlatformModePlatformCtx(operatorv1.Managed))).Should(BeTrue())
}

func TestBuildModuleCR_BasicProjection(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	platform := newPlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
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
	platform := newPlatformCtx(operatorv1.Managed)
	platform.DSC.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
	g.Expect(err).ShouldNot(HaveOccurred())

	spec, ok := u.Object["spec"].(map[string]any)
	g.Expect(ok).Should(BeTrue(), "spec is not a map")
	g.Expect(spec["mlflowEnabled"]).Should(BeTrue())
}

func TestBuildModuleCR_PlatformMode(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	platform := newPlatformModePlatformCtx(operatorv1.Managed)

	u, err := h.BuildModuleCR(context.Background(), nil, platform)
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
	_, err := h.BuildModuleCR(context.Background(), nil, nil)
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
	g.Expect(h.GetRelatedImages()).ShouldNot(ContainElement("RELATED_IMAGE_ODH_WORKBENCHES_OPERATOR_IMAGE"))
}

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	h := NewHandler()
	g.Expect(h.GetName()).Should(Equal(componentApi.WorkbenchesComponentName))
}

func TestBuildModuleCRMatchesBundledCRDSchema(t *testing.T) {
	t.Parallel()

	h := NewHandler()
	moduleCR, err := h.BuildModuleCR(context.Background(), nil, newPlatformCtx(operatorv1.Managed))
	if err != nil {
		t.Fatalf("build module CR: %v", err)
	}

	validateModuleCRAgainstBundledSchema(t, moduleCR.Object)
}

func TestBuildModuleCRMatchesBundledCRDSchemaPlatformMode(t *testing.T) {
	t.Parallel()

	h := NewHandler()
	moduleCR, err := h.BuildModuleCR(context.Background(), nil, newPlatformModePlatformCtx(operatorv1.Managed))
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
