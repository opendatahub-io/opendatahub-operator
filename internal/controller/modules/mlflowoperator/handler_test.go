//nolint:testpackage // Verifies handler internals such as defaults and projected fields.
package mlflowoperator

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	mlflowTestDSCName    = "test-dsc"
	mlflowTestGatewayURL = "gateway.apps.example.com"
	mlflowTestAppsNS     = "redhat-ods-applications"
)

func TestIsEnabled(t *testing.T) {
	handler := NewHandler()

	if handler.IsEnabled(nil) {
		t.Fatalf("expected nil platform context to disable module")
	}

	platform := &modules.PlatformContext{
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{},
		},
	}
	if handler.IsEnabled(platform) {
		t.Fatalf("expected Removed MLflowOperator to be disabled")
	}

	platform.DSC.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed
	if !handler.IsEnabled(platform) {
		t.Fatalf("expected Managed MLflowOperator to enable module")
	}
}

func TestIsEnabled_PlatformMode(t *testing.T) {
	handler := NewHandler()

	platform := &modules.PlatformContext{
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					MLflowOperator: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
	if handler.IsEnabled(platform) {
		t.Fatalf("expected Removed MLflowOperator platform mode to disable module")
	}

	platform.Platform.Spec.Modules.MLflowOperator.ManagementState = operatorv1.Managed
	if !handler.IsEnabled(platform) {
		t.Fatalf("expected Managed MLflowOperator platform mode to enable module")
	}
}

func TestGetOperatorManifests(t *testing.T) {
	handler := NewHandler()

	odhManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.OpenDataHub},
	})
	if len(odhManifests.Manifests) != 1 || odhManifests.Manifests[0].SourcePath != "overlays/odh" {
		t.Fatalf("expected ODH overlay, got %#v", odhManifests.Manifests)
	}

	rhoaiManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.SelfManagedRhoai},
	})
	if len(rhoaiManifests.Manifests) != 1 || rhoaiManifests.Manifests[0].SourcePath != "overlays/rhoai" {
		t.Fatalf("expected RHOAI overlay, got %#v", rhoaiManifests.Manifests)
	}

	managedRhoaiManifests := handler.GetOperatorManifests(&modules.PlatformContext{
		Release: common.Release{Name: cluster.ManagedRhoai},
	})
	if len(managedRhoaiManifests.Manifests) != 1 || managedRhoaiManifests.Manifests[0].SourcePath != "overlays/rhoai" {
		t.Fatalf("expected managed RHOAI overlay, got %#v", managedRhoaiManifests.Manifests)
	}
}

func TestBuildModuleCR(t *testing.T) {
	handler := NewHandler()

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName},
	}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	moduleCR, err := handler.BuildModuleCR(t.Context(), nil, &modules.PlatformContext{
		ApplicationsNamespace: mlflowTestAppsNS,
		GatewayDomain:         mlflowTestGatewayURL,
		Release:               common.Release{Name: cluster.SelfManagedRhoai},
		DSC:                   dsc,
	})
	if err != nil {
		t.Fatalf("build module CR: %v", err)
	}

	if moduleCR.GetName() != crName {
		t.Fatalf("expected CR name %q, got %q", crName, moduleCR.GetName())
	}

	spec, ok := moduleCR.Object["spec"].(map[string]any)
	if !ok {
		t.Fatalf("expected unstructured spec map, got %#v", moduleCR.Object["spec"])
	}
	if _, found := spec["applicationsNamespace"]; found {
		t.Fatalf("expected applications namespace to stay deployment-scoped, got %#v", spec["applicationsNamespace"])
	}
	if spec["gatewayName"] != defaultGatewayName {
		t.Fatalf("expected gatewayName %q, got %#v", defaultGatewayName, spec["gatewayName"])
	}
	if spec["sectionTitle"] != "OpenShift Self Managed Services" {
		t.Fatalf("expected RHOAI section title, got %#v", spec["sectionTitle"])
	}

	gateway, ok := spec["gateway"].(map[string]any)
	if !ok || gateway["domain"] != mlflowTestGatewayURL {
		t.Fatalf("expected gateway domain projection, got %#v", spec["gateway"])
	}
}

func TestBuildModuleCR_PlatformMode(t *testing.T) {
	handler := NewHandler()

	moduleCR, err := handler.BuildModuleCR(t.Context(), nil, &modules.PlatformContext{
		ApplicationsNamespace: mlflowTestAppsNS,
		Release:               common.Release{Name: cluster.SelfManagedRhoai},
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					MLflowOperator: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build module CR in platform mode: %v", err)
	}

	spec, ok := moduleCR.Object["spec"].(map[string]any)
	if !ok {
		t.Fatalf("expected unstructured spec map, got %#v", moduleCR.Object["spec"])
	}
	if spec["gatewayName"] != defaultGatewayName {
		t.Fatalf("expected gatewayName %q, got %#v", defaultGatewayName, spec["gatewayName"])
	}
	if spec["sectionTitle"] != "OpenShift Self Managed Services" {
		t.Fatalf("expected RHOAI section title, got %#v", spec["sectionTitle"])
	}
	if _, found := spec["gateway"]; found {
		t.Fatalf("expected empty gateway projection without platform gateway domain, got %#v", spec["gateway"])
	}

	if got := moduleCR.GetAnnotations()[annotations.ManagementStateAnnotation]; got != string(operatorv1.Managed) {
		t.Fatalf("expected management state annotation %q, got %q", operatorv1.Managed, got)
	}
}

func TestBuildModuleCRMatchesVendoredCRDSchema(t *testing.T) {
	t.Parallel()

	handler := NewHandler()
	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: mlflowTestDSCName},
	}
	dsc.Spec.Components.MLflowOperator.ManagementState = operatorv1.Managed

	moduleCR, err := handler.BuildModuleCR(t.Context(), nil, &modules.PlatformContext{
		ApplicationsNamespace: mlflowTestAppsNS,
		GatewayDomain:         mlflowTestGatewayURL,
		Release:               common.Release{Name: cluster.SelfManagedRhoai},
		DSC:                   dsc,
	})
	if err != nil {
		t.Fatalf("build module CR: %v", err)
	}

	schema := loadMLflowOperatorSchema(t)
	validator, _, err := apiextensionsvalidation.NewSchemaValidator(schema)
	if err != nil {
		t.Fatalf("build schema validator: %v", err)
	}

	errs := apiextensionsvalidation.ValidateCustomResource(field.NewPath("mlflowOperator"), moduleCR.Object, validator)
	if len(errs) > 0 {
		t.Fatalf("module CR does not match vendored CRD schema: %v", errs.ToAggregate())
	}
}

func TestBuildModuleCRMatchesVendoredCRDSchemaPlatformMode(t *testing.T) {
	t.Parallel()

	handler := NewHandler()
	moduleCR, err := handler.BuildModuleCR(t.Context(), nil, &modules.PlatformContext{
		ApplicationsNamespace: mlflowTestAppsNS,
		Release:               common.Release{Name: cluster.SelfManagedRhoai},
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					MLflowOperator: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build module CR in platform mode: %v", err)
	}

	schema := loadMLflowOperatorSchema(t)
	validator, _, err := apiextensionsvalidation.NewSchemaValidator(schema)
	if err != nil {
		t.Fatalf("build schema validator: %v", err)
	}

	errs := apiextensionsvalidation.ValidateCustomResource(field.NewPath("mlflowOperator"), moduleCR.Object, validator)
	if len(errs) > 0 {
		t.Fatalf("platform mode module CR does not match vendored CRD schema: %v", errs.ToAggregate())
	}
}

func loadMLflowOperatorSchema(t *testing.T) *apiextensions.JSONSchemaProps {
	t.Helper()

	crdPath := filepath.Join(
		"..", "..", "..", "..",
		"opt", "manifests", "mlflowoperator", "crd", "bases",
		"components.platform.opendatahub.io_mlflowoperators.yaml",
	)

	data, err := os.ReadFile(crdPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("skipping schema validation test; bundled MLflowOperator CRD not found at %s", crdPath)
		}
		t.Fatalf("read bundled MLflowOperator CRD: %v", err)
	}

	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatalf("unmarshal bundled MLflowOperator CRD: %v", err)
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
		t.Fatal("missing storage schema in bundled MLflowOperator CRD")
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
