package modules_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensionsinternal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/pruning"
	apiextensionsvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

// testPlatformContexts returns PlatformContext values for each supported
// platform, suitable for rendering manifests and building module CRs in tests.
func testPlatformContexts(chartsBasePath, manifestsBasePath string) []*modules.PlatformContext {
	return []*modules.PlatformContext{
		testDSCPlatformContext(cluster.OpenDataHub, chartsBasePath, manifestsBasePath),
	}
}

func testDSCPlatformContext(platform common.Platform, chartsBasePath, manifestsBasePath string) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "test-ns",
		ChartsBasePath:        chartsBasePath,
		ManifestsBasePath:     manifestsBasePath,
		Release:               common.Release{Name: platform},
		DSC: &dscv2.DataScienceCluster{
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					AIGateway: componentApi.DSCAIGateway{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
					},
				},
			},
		},
		DSCI: &dsciv2.DSCInitialization{
			Spec: dsciv2.DSCInitializationSpec{
				Monitoring: serviceApi.DSCIMonitoring{
					ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
			},
		},
	}
}

func testPlatformModePlatformContext(chartsBasePath, manifestsBasePath string) *modules.PlatformContext {
	return &modules.PlatformContext{
		ApplicationsNamespace: "test-ns",
		ChartsBasePath:        chartsBasePath,
		ManifestsBasePath:     manifestsBasePath,
		Release:               common.Release{Name: cluster.OpenDataHub},
		Platform: &configv1alpha1.Platform{
			Spec: configv1alpha1.PlatformSpec{
				Modules: configv1alpha1.PlatformModules{
					AIGateway:  common.ManagementSpec{ManagementState: operatorv1.Managed},
					Monitoring: common.ManagementSpec{ManagementState: operatorv1.Managed},
				},
			},
		},
	}
}

// loadCRDFromArtifacts discovers and loads the CRD for the given GVK from the
// opt/ artifact tree. It searches:
//   - opt/manifests/<name>/crd/bases/*.yaml (Kustomize modules)
//   - opt/charts/<name>/crds/*.yaml         (Helm modules)
//
// Returns nil if no matching CRD is found (caller should skip).
func loadCRDFromArtifacts(t *testing.T, handler modules.ModuleHandler, manifestsRoot, chartsRoot string) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()

	targetGVK := handler.GetGVK()
	name := handler.GetName()

	manifests := handler.GetOperatorManifests(&modules.PlatformContext{
		ChartsBasePath:    chartsRoot,
		ManifestsBasePath: manifestsRoot,
		Release:           common.Release{Name: cluster.OpenDataHub},
	})

	searchPaths := []string{
		filepath.Join(manifestsRoot, name, "crd", "bases"),
		filepath.Join(manifestsRoot, name, "crd"),
		filepath.Join(chartsRoot, name, "crds"),
	}

	for _, mi := range manifests.Manifests {
		searchPaths = append(searchPaths,
			filepath.Join(mi.Path, "crd", "bases"),
			filepath.Join(mi.Path, "crd"),
		)
	}
	for _, chart := range manifests.HelmCharts {
		searchPaths = append(searchPaths, filepath.Join(chart.Chart, "crds"))
	}

	for _, dir := range searchPaths {
		crd := findCRDInDir(t, dir, targetGVK)
		if crd != nil {
			return crd
		}
	}

	return nil
}

func findCRDInDir(t *testing.T, dir string, targetGVK schema.GroupVersionKind) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.UnmarshalStrict(data, crd); err != nil {
			continue
		}

		if crd.Spec.Group != targetGVK.Group {
			continue
		}

		if crd.Spec.Names.Kind != targetGVK.Kind {
			continue
		}

		return crd
	}

	return nil
}

// getOpenAPIV3Schema extracts the OpenAPI v3 schema for the given version from
// a CRD. Falls back to the first version if no exact match is found.
func getOpenAPIV3Schema(crd *apiextensionsv1.CustomResourceDefinition, version string) *apiextensionsv1.JSONSchemaProps {
	for _, v := range crd.Spec.Versions {
		if v.Name == version && v.Schema != nil {
			return v.Schema.OpenAPIV3Schema
		}
	}

	if len(crd.Spec.Versions) > 0 && crd.Spec.Versions[0].Schema != nil {
		return crd.Spec.Versions[0].Schema.OpenAPIV3Schema
	}

	return nil
}

func TestModuleCRSchemaCompliance(t *testing.T) {
	chartsRoot := os.Getenv("DEFAULT_CHARTS_PATH")
	if chartsRoot == "" {
		chartsRoot = filepath.Join("..", "..", "..", "opt", "charts")
	}

	manifestsRoot := os.Getenv("DEFAULT_MANIFESTS_PATH")
	if manifestsRoot == "" {
		manifestsRoot = filepath.Join("..", "..", "..", "opt", "manifests")
	}

	absChartsRoot, err := filepath.Abs(chartsRoot)
	if err != nil {
		t.Fatalf("failed to resolve charts root %s: %v", chartsRoot, err)
	}

	absManifestsRoot, err := filepath.Abs(manifestsRoot)
	if err != nil {
		t.Fatalf("failed to resolve manifests root %s: %v", manifestsRoot, err)
	}

	handlers := moduleHandlers()
	if len(handlers) == 0 {
		t.Skipf("no module handlers registered; skipping CRD schema compliance test")
	}

	dscCtx := testDSCPlatformContext(cluster.OpenDataHub, absChartsRoot, absManifestsRoot)
	platformCtx := testPlatformModePlatformContext(absChartsRoot, absManifestsRoot)

	testedCount := 0

	for _, handler := range handlers {
		crd := loadCRDFromArtifacts(t, handler, absManifestsRoot, absChartsRoot)
		if crd == nil {
			t.Logf("no CRD artifact found for module %s (GVK: %s), skipping schema validation", handler.GetName(), handler.GetGVK())
			continue
		}

		v3Schema := getOpenAPIV3Schema(crd, handler.GetGVK().Version)
		if v3Schema == nil {
			t.Logf("no OpenAPI v3 schema found in CRD for module %s, skipping", handler.GetName())
			continue
		}

		internalSchema := &apiextensionsinternal.JSONSchemaProps{}
		if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(v3Schema, internalSchema, nil); err != nil {
			t.Fatalf("failed to convert v1 schema to internal for module %s: %v", handler.GetName(), err)
		}

		validator, _, err := apiextensionsvalidation.NewSchemaValidator(internalSchema)
		if err != nil {
			t.Fatalf("failed to create schema validator for module %s: %v", handler.GetName(), err)
		}

		ss, err := structuralschema.NewStructural(internalSchema)
		if err != nil {
			t.Fatalf("failed to create structural schema for module %s: %v", handler.GetName(), err)
		}

		contexts := map[string]*modules.PlatformContext{
			"dsc":      dscCtx,
			"platform": platformCtx,
		}

		for ctxName, ctx := range contexts {
			if !handler.IsEnabled(ctx) {
				continue
			}

			testedCount++

			t.Run(handler.GetName()+"/"+ctxName, func(t *testing.T) {
				g := NewWithT(t)

				cr, err := handler.BuildModuleCR(context.Background(), nil, ctx)
				g.Expect(err).ShouldNot(HaveOccurred(), "BuildModuleCR failed for %s in %s mode", handler.GetName(), ctxName)
				g.Expect(cr).ShouldNot(BeNil(), "BuildModuleCR returned nil for %s in %s mode", handler.GetName(), ctxName)

				errs := apiextensionsvalidation.ValidateCustomResource(field.NewPath(""), cr.Object, validator)
				g.Expect(errs).Should(BeEmpty(),
					"BuildModuleCR output for %s (%s mode) does not conform to CRD schema:\n%s",
					handler.GetName(), ctxName, errs.ToAggregate())

				prunedFields := pruning.PruneWithOptions(cr.Object, ss, true, structuralschema.UnknownFieldPathOptions{
					TrackUnknownFieldPaths: true,
				})
				g.Expect(prunedFields).Should(BeEmpty(),
					"BuildModuleCR output for %s (%s mode) contains fields not in CRD schema (would be pruned by API server): %v",
					handler.GetName(), ctxName, prunedFields)
			})
		}
	}

	if testedCount == 0 {
		t.Skipf("no module CRD artifacts available for schema validation (run get_all_manifests.sh first)")
	}
}
