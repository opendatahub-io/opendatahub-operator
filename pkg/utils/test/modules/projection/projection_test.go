package projection_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/modules/projection"

	. "github.com/onsi/gomega"
)

const (
	synthGroup   = "synth.platform.opendatahub.io"
	synthVersion = "v1alpha1"
	synthKind    = "Synthmodule"
	synthPlural  = "synthmodules"
	synthCRDName = synthPlural + "." + synthGroup
)

func synthGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: synthGroup, Version: synthVersion, Kind: synthKind}
}

// buildSyntheticCRD renders the synthetic CRD used for harness self-tests.
// It carries a required field, an enum with a default, an integer with a
// default, a boolean, a nested object with an array, a free-form empty
// object, and a CEL rule, so admission behavior is observable without any
// downloaded module asset. mutate may alter the CRD for negative cases.
func buildSyntheticCRD(t *testing.T, mutate func(*apiextensionsv1.CustomResourceDefinition)) []byte {
	t.Helper()

	crd := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition"},
		ObjectMeta: metav1.ObjectMeta{Name: synthCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: synthGroup,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     synthKind,
				ListKind: synthKind + "List",
				Plural:   synthPlural,
				Singular: "synthmodule",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    synthVersion,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec": {
								Type:     "object",
								Required: []string{"name"},
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"name": {
										Type:      "string",
										MinLength: int64Ptr(1),
									},
									"mode": {
										Type:    "string",
										Enum:    []apiextensionsv1.JSON{{Raw: []byte(`"fast"`)}, {Raw: []byte(`"slow"`)}},
										Default: &apiextensionsv1.JSON{Raw: []byte(`"fast"`)},
									},
									"replicas": {
										Type:    "integer",
										Default: &apiextensionsv1.JSON{Raw: []byte("1")},
									},
									"enabled": {Type: "boolean"},
									"nested": {
										Type: "object",
										Properties: map[string]apiextensionsv1.JSONSchemaProps{
											"items": {Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"},
											}},
										},
									},
									"emptyObject": {Type: "object"},
								},
								XValidations: []apiextensionsv1.ValidationRule{{
									Rule:    "self.replicas >= 1",
									Message: "replicas must be at least 1",
								}},
							},
						},
					},
				},
			}},
		},
	}

	if mutate != nil {
		mutate(crd)
	}

	data, err := k8syaml.Marshal(crd)
	if err != nil {
		t.Fatalf("rendering synthetic CRD: %v", err)
	}

	return data
}

func int64Ptr(v int64) *int64 {
	p := new(int64)
	*p = v
	return p
}

// synthCR builds a synthetic module CR with the given spec.
func synthCR(t *testing.T, name string, spec map[string]any) *unstructured.Unstructured {
	t.Helper()

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(synthGVK())
	u.SetName(name)
	if spec != nil {
		u.Object["spec"] = spec
	}

	return u
}

// stageSyntheticChart writes the synthetic chart assets into a temporary
// base directory and returns the module config selecting them. It requires
// no downloaded module assets.
func stageSyntheticChart(t *testing.T) modules.ModuleConfig {
	t.Helper()

	g := NewWithT(t)

	base := t.TempDir()
	t.Setenv(projection.DefaultChartsPathEnv, base)

	crdDir := filepath.Join(base, "synthmod", "crds")
	g.Expect(os.MkdirAll(crdDir, 0o755)).Should(Succeed(), "creating synthetic chart directory")
	g.Expect(os.WriteFile(filepath.Join(crdDir, "crd.yaml"), buildSyntheticCRD(t, nil), 0o600)).Should(Succeed())

	return modules.ModuleConfig{
		Name:     "synth",
		GVK:      synthGVK(),
		CRName:   "default-synth",
		ChartDir: "synthmod",
	}
}

// newSyntheticEnv starts a projection envtest from the staged synthetic
// chart assets.
func newSyntheticEnv(t *testing.T) (*envt.EnvT, modules.ModuleConfig) {
	t.Helper()

	cfg := stageSyntheticChart(t)

	return projection.NewEnv(t, cfg, "crds/crd.yaml"), cfg
}

// newSyntheticRunner returns a runner over the staged synthetic chart. Its
// builder projection is irrelevant to admission-focused tests, which submit
// their own objects.
func newSyntheticRunner(t *testing.T) *projection.Runner {
	t.Helper()

	builder := &fakeModuleCRBuilder{
		cfg: stageSyntheticChart(t),
		build: func(context.Context, client.Client, *modules.DSCContext, *modules.ModuleCRConfig) (*unstructured.Unstructured, error) {
			return synthCR(t, "default-synth", map[string]any{"name": "instance"}), nil
		},
	}

	return projection.NewRunner(t, builder, "crds/crd.yaml")
}

func TestResolveAssetBase(t *testing.T) {
	const root = "/project/root"

	t.Run("unset selects the default relative to the project root", func(t *testing.T) {
		g := NewWithT(t)
		t.Setenv(projection.DefaultChartsPathEnv, "")
		g.Expect(projection.ResolveAssetBase(root, projection.DefaultChartsPathEnv, filepath.Join("opt", "charts"))).
			Should(Equal(filepath.Join(root, "opt", "charts")))
	})

	t.Run("relative override is resolved from the project root", func(t *testing.T) {
		g := NewWithT(t)
		t.Setenv(projection.DefaultChartsPathEnv, "custom/assets")
		g.Expect(projection.ResolveAssetBase(root, projection.DefaultChartsPathEnv, filepath.Join("opt", "charts"))).
			Should(Equal(filepath.Join(root, "custom", "assets")))
	})

	t.Run("whitespace override is trimmed", func(t *testing.T) {
		g := NewWithT(t)
		t.Setenv(projection.DefaultChartsPathEnv, "  alt/assets  ")
		g.Expect(projection.ResolveAssetBase(root, projection.DefaultChartsPathEnv, filepath.Join("opt", "charts"))).
			Should(Equal(filepath.Join(root, "alt", "assets")))
	})

	t.Run("absolute override is used as-is", func(t *testing.T) {
		g := NewWithT(t)
		absolute := t.TempDir()
		t.Setenv(projection.DefaultChartsPathEnv, absolute)
		g.Expect(projection.ResolveAssetBase(root, projection.DefaultChartsPathEnv, filepath.Join("opt", "charts"))).
			Should(Equal(absolute))
	})
}

func TestResolveCRDPath(t *testing.T) {
	gvk := synthGVK()

	t.Run("both bases configured is an error", func(t *testing.T) {
		g := NewWithT(t)
		path, err := projection.ResolveCRDPath("/root", modules.ModuleConfig{
			Name: "synth", GVK: gvk, ChartDir: "chart", ManifestDir: "manifest",
		}, "crds/crd.yaml")
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("exactly one"))
		g.Expect(path).Should(BeEmpty())
	})

	t.Run("neither base configured is an error", func(t *testing.T) {
		g := NewWithT(t)
		path, err := projection.ResolveCRDPath("/root", modules.ModuleConfig{Name: "synth", GVK: gvk}, "crds/crd.yaml")
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("one of"))
		g.Expect(path).Should(BeEmpty())
	})

	t.Run("chart base is used for chart-based modules", func(t *testing.T) {
		g := NewWithT(t)
		base := t.TempDir()
		t.Setenv(projection.DefaultChartsPathEnv, base)
		path, err := projection.ResolveCRDPath("/root", modules.ModuleConfig{
			Name: "synth", GVK: gvk, ChartDir: "synthmod",
		}, "crds/crd.yaml")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(path).Should(Equal(filepath.Join(base, "synthmod", "crds", "crd.yaml")))
	})

	t.Run("manifest base is used for manifest-based modules", func(t *testing.T) {
		g := NewWithT(t)
		base := t.TempDir()
		t.Setenv(projection.DefaultManifestsPathEnv, base)
		path, err := projection.ResolveCRDPath("/root", modules.ModuleConfig{
			Name: "synth", GVK: gvk, ManifestDir: "synthmod",
		}, "crd/bases/crd.yaml")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(path).Should(Equal(filepath.Join(base, "synthmod", "crd", "bases", "crd.yaml")))
	})

	t.Run("manifest base defaults to opt/manifests", func(t *testing.T) {
		g := NewWithT(t)
		t.Setenv(projection.DefaultManifestsPathEnv, "")
		path, err := projection.ResolveCRDPath("/root", modules.ModuleConfig{
			Name: "synth", GVK: gvk, ManifestDir: "synthmod",
		}, "crd/bases/crd.yaml")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(path).Should(Equal(filepath.Join("/root", "opt", "manifests", "synthmod", "crd", "bases", "crd.yaml")))
	})
}

func TestSelectModuleCRD(t *testing.T) {
	cfg := modules.ModuleConfig{Name: "synth", GVK: synthGVK(), CRName: "default-synth"}

	writeFile := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "crd.yaml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing CRD file: %v", err)
		}
		return path
	}

	t.Run("missing file fails with preparation guidance", func(t *testing.T) {
		g := NewWithT(t)
		path := filepath.Join(t.TempDir(), "absent.yaml")
		_, err := projection.SelectModuleCRD(cfg, path)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring(path))
		g.Expect(err.Error()).Should(ContainSubstring("make get-manifests"))
		g.Expect(err.Error()).Should(ContainSubstring(synthGVK().String()))
	})

	t.Run("malformed YAML fails", func(t *testing.T) {
		g := NewWithT(t)
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, "not: [closed"))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("malformed"))
	})

	t.Run("malformed CRD document fails", func(t *testing.T) {
		g := NewWithT(t)
		content := "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nspec: 42\n"
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, content))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("malformed CustomResourceDefinition"))
	})

	t.Run("wrong group has no match", func(t *testing.T) {
		g := NewWithT(t)
		data := buildSyntheticCRD(t, func(crd *apiextensionsv1.CustomResourceDefinition) {
			crd.Spec.Group = "other.platform.opendatahub.io"
		})
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, string(data)))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("no CRD matching"))
	})

	t.Run("wrong kind has no match", func(t *testing.T) {
		g := NewWithT(t)
		data := buildSyntheticCRD(t, func(crd *apiextensionsv1.CustomResourceDefinition) {
			crd.Spec.Names.Kind = "Othermodule"
		})
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, string(data)))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("no CRD matching"))
	})

	t.Run("unserved version has no match", func(t *testing.T) {
		g := NewWithT(t)
		data := buildSyntheticCRD(t, func(crd *apiextensionsv1.CustomResourceDefinition) {
			crd.Spec.Versions[0].Served = false
		})
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, string(data)))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("no CRD matching"))
	})

	t.Run("different version has no match", func(t *testing.T) {
		g := NewWithT(t)
		data := buildSyntheticCRD(t, func(crd *apiextensionsv1.CustomResourceDefinition) {
			crd.Spec.Versions[0].Name = "v1beta1"
		})
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, string(data)))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("no CRD matching"))
	})

	t.Run("duplicate matches are ambiguous", func(t *testing.T) {
		g := NewWithT(t)
		data := string(buildSyntheticCRD(t, nil))
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, data+"---\n"+data))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("ambiguous"))
		g.Expect(err.Error()).Should(ContainSubstring("expected exactly one"))
	})

	t.Run("empty schema is rejected", func(t *testing.T) {
		g := NewWithT(t)
		data := buildSyntheticCRD(t, func(crd *apiextensionsv1.CustomResourceDefinition) {
			crd.Spec.Versions[0].Schema = nil
		})
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, string(data)))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("no schema"))
	})

	t.Run("namespaced scope is rejected", func(t *testing.T) {
		g := NewWithT(t)
		data := buildSyntheticCRD(t, func(crd *apiextensionsv1.CustomResourceDefinition) {
			crd.Spec.Scope = apiextensionsv1.NamespaceScoped
		})
		_, err := projection.SelectModuleCRD(cfg, writeFile(t, string(data)))
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("cluster-scoped"))
	})

	t.Run("non-CRD documents are ignored and the match is returned", func(t *testing.T) {
		g := NewWithT(t)
		foreign := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unrelated\n"
		data := string(buildSyntheticCRD(t, nil))
		path := writeFile(t, foreign+"---\n"+data)

		crd, err := projection.SelectModuleCRD(cfg, path)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(crd.Name).Should(Equal(synthCRDName))
	})
}

func TestNewEnvInstallsOnlySelectedCRD(t *testing.T) {
	et, cfg := newSyntheticEnv(t)
	g := NewWithT(t)

	// No manager, controllers, or webhooks are started.
	g.Expect(et.Manager()).Should(BeNil())

	// Exactly one CRD is installed, and it is the selected synthetic one.
	crds := &apiextensionsv1.CustomResourceDefinitionList{}
	g.Expect(et.Client().List(t.Context(), crds)).Should(Succeed())
	g.Expect(crds.Items).Should(HaveLen(1))
	g.Expect(crds.Items[0].Name).Should(Equal(synthCRDName))

	// The installed CRD is established and unchanged: the CEL rule survives.
	established := false
	for _, condition := range crds.Items[0].Status.Conditions {
		if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
			established = true
		}
	}
	g.Expect(established).Should(BeTrue(), "installed CRD should be established")

	schema := crds.Items[0].Spec.Versions[0].Schema.OpenAPIV3Schema
	specSchema := schema.Properties["spec"]
	g.Expect(specSchema.XValidations).Should(HaveLen(1))
	g.Expect(specSchema.XValidations[0].Rule).Should(Equal("self.replicas >= 1"))

	// Teardown is registered as a checked cleanup: a failing Stop() fails
	// this test after it returns.
	g.Expect(cfg.CRName).Should(Equal("default-synth"))
}

func TestAdmit(t *testing.T) {
	r := newSyntheticRunner(t)

	t.Run("retains projected fields without mutating the input", func(t *testing.T) {
		g := NewWithT(t)

		projected := synthCR(t, "default-synth", map[string]any{
			"name":        "primary",
			"mode":        "slow",
			"replicas":    int64(2),
			"enabled":     false,
			"nested":      map[string]any{"items": []any{"alpha", "beta"}},
			"emptyObject": map[string]any{},
		})
		projected.SetAnnotations(map[string]string{"test.opendatahub.io/owner": "projection-test"})
		before := projected.DeepCopy()

		admitted, err := r.Admit(t.Context(), projected)
		g.Expect(err).ShouldNot(HaveOccurred())

		// The handler's object is preserved: admission works on a deep copy.
		g.Expect(projected).Should(Equal(before))

		// Explicitly projected values survive admission independently of the
		// retention comparison, including false and zero values, arrays,
		// empty objects, and nested data.
		g.Expect(nestedField(t, admitted, "spec", "name")).Should(Equal("primary"))
		g.Expect(nestedField(t, admitted, "spec", "mode")).Should(Equal("slow"))
		g.Expect(nestedField(t, admitted, "spec", "replicas")).Should(Equal(int64(2)))
		g.Expect(nestedField(t, admitted, "spec", "enabled")).Should(BeFalse())
		g.Expect(nestedField(t, admitted, "spec", "nested", "items")).Should(ContainElements("alpha", "beta"))
		g.Expect(nestedField(t, admitted, "spec", "emptyObject")).Should(Equal(map[string]any{}))
		g.Expect(admitted.GetAnnotations()).Should(HaveKeyWithValue("test.opendatahub.io/owner", "projection-test"))
	})

	t.Run("applies schema defaults and returns them", func(t *testing.T) {
		g := NewWithT(t)

		projected := synthCR(t, "defaults-synth", map[string]any{"name": "primary"})
		admitted, err := r.Admit(t.Context(), projected)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(nestedField(t, admitted, "spec", "mode")).Should(Equal("fast"))
		g.Expect(nestedField(t, admitted, "spec", "replicas")).Should(Equal(int64(1)))
	})

	t.Run("repeated singleton submissions do not collide", func(t *testing.T) {
		g := NewWithT(t)

		first, err := r.Admit(t.Context(), synthCR(t, "default-synth", map[string]any{"name": "first"}))
		g.Expect(err).ShouldNot(HaveOccurred())
		second, err := r.Admit(t.Context(), synthCR(t, "default-synth", map[string]any{"name": "second"}))
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(nestedField(t, first, "spec", "name")).Should(Equal("first"))
		g.Expect(nestedField(t, second, "spec", "name")).Should(Equal("second"))
	})

	t.Run("dry-run leaves no persistent instance", func(t *testing.T) {
		g := NewWithT(t)

		_, err := r.Admit(t.Context(), synthCR(t, "default-synth", map[string]any{"name": "primary"}))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Server-side dry-run create still detects name collisions: a
		// persisted instance would reject this submission with
		// AlreadyExists, so its success proves nothing was stored.
		_, err = r.Admit(t.Context(), synthCR(t, "default-synth", map[string]any{"name": "second"}))
		g.Expect(err).ShouldNot(HaveOccurred(), "dry-run create must not persist the module CR")
	})
}

func TestAdmissionRejections(t *testing.T) {
	r := newSyntheticRunner(t)

	tests := map[string]struct {
		spec     map[string]any
		contains string
	}{
		"invalid enum value":     {spec: map[string]any{"name": "n", "mode": "turbo"}, contains: "turbo"},
		"invalid field type":     {spec: map[string]any{"name": "n", "replicas": "three"}, contains: "replicas"},
		"missing required field": {spec: map[string]any{"mode": "fast"}, contains: "name"},
		"violated CEL rule":      {spec: map[string]any{"name": "n", "replicas": int64(0)}, contains: "replicas must be at least 1"},
		"unknown field (strict)": {spec: map[string]any{"name": "n", "bogus": true}, contains: "bogus"},
		"minLength violation":    {spec: map[string]any{"name": ""}, contains: "name"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			obj := synthCR(t, "rejected-synth", tc.spec)
			before := obj.DeepCopy()

			err := r.AdmitCreate(t.Context(), obj)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring(tc.contains))

			// A rejected submission leaves the input object unchanged.
			g.Expect(obj).Should(Equal(before))
		})
	}
}

// nestedField returns an admitted field value or fails the test when absent.
func nestedField(t *testing.T, obj *unstructured.Unstructured, fields ...string) any {
	t.Helper()

	value, found, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	if err != nil {
		t.Fatalf("reading field %s: %v", strings.Join(fields, "."), err)
	}
	if !found {
		t.Fatalf("expected field %s to be present", strings.Join(fields, "."))
	}

	return value
}

// fakeModuleCRBuilder implements projection.ModuleCRHandler around a fixed
// projection function and configuration.
type fakeModuleCRBuilder struct {
	cfg   modules.ModuleConfig
	build func(
		ctx context.Context,
		cli client.Client,
		dscCtx *modules.DSCContext,
		cfg *modules.ModuleCRConfig,
	) (*unstructured.Unstructured, error)
}

func (f *fakeModuleCRBuilder) BuildModuleCR(
	ctx context.Context,
	cli client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	return f.build(ctx, cli, dscCtx, cfg)
}

func (f *fakeModuleCRBuilder) GetModuleConfig() modules.ModuleConfig {
	return f.cfg
}

func TestRunnerValidate(t *testing.T) {
	builder := &fakeModuleCRBuilder{
		cfg: stageSyntheticChart(t),
		build: func(context.Context, client.Client, *modules.DSCContext, *modules.ModuleCRConfig) (*unstructured.Unstructured, error) {
			return synthCR(t, "default-synth", map[string]any{"name": "instance"}), nil
		},
	}

	r := projection.NewRunner(t, builder, "crds/crd.yaml")

	dscCtx := &modules.DSCContext{}

	// No options: admission-only. The strict dry-run create is the whole
	// validation; no value assertion runs.
	r.Validate(t, projection.ModuleCRSpec{DSCCtx: *dscCtx})

	// Pre applies to the raw projection: the builder emits only spec.name,
	// so the schema-defaulted mode is absent before admission.
	r.Validate(t, projection.ModuleCRSpec{DSCCtx: *dscCtx},
		projection.WithPreAdmissionValidation(jq.Match(`.spec.mode == null`)))

	// Post applies to the admitted response, which carries the schema
	// defaults the raw output lacks.
	r.Validate(t, projection.ModuleCRSpec{DSCCtx: *dscCtx},
		projection.WithPostAdmissionValidation(jq.Match(`.spec.mode == "fast"`)))

	// Both phases compose in one case: the handler omits mode and the CRD
	// defaults it to fast.
	r.Validate(t, projection.ModuleCRSpec{DSCCtx: *dscCtx},
		projection.AdmissionValidation{
			Pre:  jq.Match(`.spec.mode == null`),
			Post: jq.Match(`.spec.mode == "fast"`),
		})

	// An AdmissionValidation literal composes with With* options instead of
	// clobbering them (ApplyTo skips the matchers it does not carry).
	r.Validate(t, projection.ModuleCRSpec{DSCCtx: *dscCtx},
		projection.WithPostAdmissionValidation(jq.Match(`.spec.mode == "fast"`)),
		projection.AdmissionValidation{Pre: jq.Match(`.spec.mode == null`)},
	)
}

func TestValidateOptionsApplyTo(t *testing.T) {
	pre := jq.Match(`.spec.pre == true`)
	post := jq.Match(`.spec.post == true`)

	t.Run("empty literal sets nothing", func(t *testing.T) {
		g := NewWithT(t)
		var o projection.ValidateOptions
		projection.AdmissionValidation{}.ApplyTo(&o)
		g.Expect(o.Pre).Should(BeNil())
		g.Expect(o.Post).Should(BeNil())
	})

	t.Run("partial literal skips nil matchers", func(t *testing.T) {
		g := NewWithT(t)
		o := projection.ValidateOptions{Pre: pre}
		projection.AdmissionValidation{Post: post}.ApplyTo(&o)
		g.Expect(o.Pre).Should(Equal(pre))
		g.Expect(o.Post).Should(Equal(post))
	})

	t.Run("With options set their own phase", func(t *testing.T) {
		g := NewWithT(t)
		var o projection.ValidateOptions
		projection.WithPreAdmissionValidation(pre).ApplyTo(&o)
		projection.WithPostAdmissionValidation(post).ApplyTo(&o)
		g.Expect(o.Pre).Should(Equal(pre))
		g.Expect(o.Post).Should(Equal(post))
	})
}
