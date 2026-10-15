// Package projection provides a shared envtest harness for module CR projection
// admission tests. Handler tests use NewEnv to start an isolated
// API server with only the module's real CRD installed, and Admit
// to submit a handler's BuildModuleCR output as a strict dry-run create: the
// API-server error is the validation result, and the admitted response
// (schema defaults applied) is returned for case-specific assertions.
package projection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

const (
	// DefaultChartsPathEnv overrides the base directory for chart-based modules.
	DefaultChartsPathEnv = "DEFAULT_CHARTS_PATH"

	// DefaultManifestsPathEnv overrides the base directory for manifest-based modules.
	DefaultManifestsPathEnv = "DEFAULT_MANIFESTS_PATH"

	defaultChartsRelPath    = "opt/charts"
	defaultManifestsRelPath = "opt/manifests"
)

// NewEnv starts an isolated envtest API server with only the
// module's real, unchanged CRD installed. The CRD is resolved from the local
// downloaded assets selected by cfg (ChartDir or ManifestDir) plus
// crdRelativePath. Checked teardown is registered immediately. The
// environment starts no manager, controllers, or webhooks.
func NewEnv(t *testing.T, cfg modules.ModuleConfig, crdRelativePath string) *envt.EnvT {
	t.Helper()

	// envtest falls back to the USE_EXISTING_CLUSTER env var to target an
	// external cluster; these tests install CRDs, so the local API server is
	// forced regardless of the ambient value.
	t.Setenv("USE_EXISTING_CLUSTER", "false")

	root, err := envtestutil.FindProjectRoot()
	if err != nil {
		t.Fatalf("unable to determine project root: %v", err)
	}

	crdPath, err := ResolveCRDPath(root, cfg, crdRelativePath)
	if err != nil {
		t.Fatalf("%v", err)
	}

	crd, err := SelectModuleCRD(cfg, crdPath)
	if err != nil {
		t.Fatalf("%v", err)
	}

	// envtest loads every YAML file in the directories passed to it, so the
	// selected CRD is re-serialized into a per-test directory to install
	// only that CRD.
	stagedDoc, err := k8syaml.Marshal(crd)
	if err != nil {
		t.Fatalf("module %s: serializing CRD from %s: %v", cfg.Name, crdPath, err)
	}

	installDir := t.TempDir()
	stagedPath := filepath.Join(installDir, "crd.yaml")
	if err := os.WriteFile(stagedPath, stagedDoc, 0o600); err != nil {
		t.Fatalf("module %s: staging CRD from %s: %v", cfg.Name, crdPath, err)
	}

	et, err := envt.New(envt.WithCRDPaths(installDir))
	if err != nil {
		t.Fatalf(
			"module %s: starting envtest with CRD %s: %v "+
				"(envtest binaries missing? run 'make envtest' or 'make unit-test-operator', which provisions KUBEBUILDER_ASSETS)",
			cfg.Name, crdPath, err,
		)
	}

	t.Cleanup(func() {
		if err := et.Stop(); err != nil {
			t.Errorf("module %s: stopping projection envtest: %v", cfg.Name, err)
		}
	})

	return et
}

// ModuleCRSpec mirrors the BuildModuleCR parameters under test: the input
// DSC/DSCI context (a value; the zero value is the no-context input for
// handlers that ignore it), the optional client feeding input lookups (never
// the envtest admission client), and the module CR config, where nil is a
// valid input.
type ModuleCRSpec struct {
	DSCCtx modules.DSCContext
	Client client.Client
	Cfg    *modules.ModuleCRConfig
}

// ModuleCRBuilder is the subset of a module handler exercised by projection
// tests.
type ModuleCRBuilder interface {
	BuildModuleCR(
		ctx context.Context,
		cli client.Client,
		dscCtx *modules.DSCContext,
		cfg *modules.ModuleCRConfig,
	) (*unstructured.Unstructured, error)
}

// ModuleCRHandler is the handler surface the projection runner needs: the
// standard BuildModuleCR signature plus the configuration accessor every
// modules.BaseHandler embedder satisfies.
type ModuleCRHandler interface {
	ModuleCRBuilder
	GetModuleConfig() modules.ModuleConfig
}

// Runner drives BuildModuleCR cases for one module against its real
// CRD on an isolated envtest API server.
type Runner struct {
	env     *envt.EnvT
	handler ModuleCRHandler
	cfg     modules.ModuleConfig
}

// NewRunner starts the projection environment for handler's module
// (see NewEnv) and returns a runner validating its BuildModuleCR
// output.
func NewRunner(t *testing.T, handler ModuleCRHandler, crdRelativePath string) *Runner {
	t.Helper()

	cfg := handler.GetModuleConfig()

	return &Runner{
		env:     NewEnv(t, cfg, crdRelativePath),
		handler: handler,
		cfg:     cfg,
	}
}

// ValidateOptions configures the value assertions of one projection case.
// Pre is applied to the raw BuildModuleCR output and Post to the admitted
// API-server response, so a Pre assertion proves the handler projected the
// value while a Post assertion may also be satisfied by CRD schema
// defaulting.
type ValidateOptions struct {
	Pre  types.GomegaMatcher
	Post types.GomegaMatcher
}

// AdmissionValidation is the call-site spelling of ValidateOptions: a
// partial literal can be passed to Validate directly, bundling both phases
// in one option.
type AdmissionValidation = ValidateOptions

// ValidateOption is a single configuration for a Validate call.
type ValidateOption interface {
	ApplyTo(target *ValidateOptions)
}

type validateOptionFunc func(*ValidateOptions)

func (f validateOptionFunc) ApplyTo(o *ValidateOptions) { f(o) }

// ApplyTo sets only the matchers it carries, so a partial AdmissionValidation
// literal composes with other options instead of clobbering them.
func (o ValidateOptions) ApplyTo(target *ValidateOptions) {
	if o.Pre != nil {
		target.Pre = o.Pre
	}
	if o.Post != nil {
		target.Post = o.Post
	}
}

// WithPreAdmissionValidation returns an option applying m to the raw
// BuildModuleCR output, before admission: such assertions cannot be
// satisfied by CRD schema defaulting.
func WithPreAdmissionValidation(m types.GomegaMatcher) ValidateOption { //nolint:ireturn // Functional-options idiom
	return validateOptionFunc(func(o *ValidateOptions) { o.Pre = m })
}

// WithPostAdmissionValidation returns an option applying m to the admitted
// API-server response, after schema defaulting.
func WithPostAdmissionValidation(m types.GomegaMatcher) ValidateOption { //nolint:ireturn // Functional-options idiom
	return validateOptionFunc(func(o *ValidateOptions) { o.Post = m })
}

// Validate runs one projection case: it builds the module CR from spec,
// asserts the configured identity (GVK, CR name, cluster scope) and the
// input nonmutation on the raw output, applies the Pre matcher to that raw
// output, admits it against the real CRD as a strict dry-run create, and
// applies the Post matcher to the admitted response. With no options the
// case is admission-only: the strict dry-run create is the validation.
func (r *Runner) Validate(t *testing.T, spec ModuleCRSpec, opts ...ValidateOption) {
	t.Helper()
	g := gomega.NewWithT(t)

	var o ValidateOptions
	for _, opt := range opts {
		if opt != nil {
			opt.ApplyTo(&o)
		}
	}

	// Snapshot both inputs before the call and compare them unconditionally
	// afterward: a handler clearing or replacing its DSC/DSCI must fail the
	// check, not bypass it.
	var dscBefore, dsciBefore any
	if spec.DSCCtx.DSC != nil {
		dscBefore = spec.DSCCtx.DSC.DeepCopy()
	}
	if spec.DSCCtx.DSCI != nil {
		dsciBefore = spec.DSCCtx.DSCI.DeepCopy()
	}

	raw, err := r.handler.BuildModuleCR(t.Context(), spec.Client, &spec.DSCCtx, spec.Cfg)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	g.Expect(raw).ShouldNot(gomega.BeNil())
	g.Expect(raw.GroupVersionKind()).Should(gomega.Equal(r.cfg.GVK))
	g.Expect(raw.GetName()).Should(gomega.Equal(r.cfg.CRName))
	g.Expect(raw.GetNamespace()).Should(gomega.BeEmpty(), "module CR is cluster-scoped")

	if dscBefore != nil || spec.DSCCtx.DSC != nil {
		g.Expect(spec.DSCCtx.DSC).Should(gomega.Equal(dscBefore), "BuildModuleCR must not mutate the DSC")
	}
	if dsciBefore != nil || spec.DSCCtx.DSCI != nil {
		g.Expect(spec.DSCCtx.DSCI).Should(gomega.Equal(dsciBefore), "BuildModuleCR must not mutate the DSCI")
	}

	if o.Pre != nil {
		g.Expect(raw).Should(o.Pre, "pre-admission check (raw projection)")
	}

	admitted, err := r.Admit(t.Context(), raw)
	g.Expect(err).ShouldNot(gomega.HaveOccurred())
	if o.Post != nil {
		g.Expect(admitted).Should(o.Post, "post-admission check (schema defaults applied)")
	}
}

// Admit submits projected to the runner's envtest API server as a dry-run
// create with strict field validation, and returns the admitted server
// response for case-specific assertions. It returns an error when
// admission is rejected: with strict field validation,
// any field unknown to the real CRD schema — which would otherwise be
// silently pruned — is rejected instead, so API-server errors are the
// pruning guard. The passed object is never mutated: admission works on a
// deep copy.
func (r *Runner) Admit(ctx context.Context, projected *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if projected == nil {
		return nil, errors.New("projected module CR is nil")
	}

	submitted := projected.DeepCopy()

	if err := r.AdmitCreate(ctx, submitted); err != nil {
		return nil, fmt.Errorf(
			"module CR %s %q was rejected by API-server admission (dry-run, strict): %w",
			projected.GroupVersionKind().String(), projected.GetName(), err)
	}

	return submitted, nil
}

// AdmitCreate submits obj to the runner's envtest API server as a dry-run
// create with strict field validation. On success obj is updated in place
// with the server response, including applied schema defaults; nothing is
// persisted.
func (r *Runner) AdmitCreate(ctx context.Context, obj client.Object) error {
	return r.env.Client().Create(ctx, obj, &client.CreateOptions{
		DryRun:          []string{metav1.DryRunAll},
		FieldValidation: metav1.FieldValidationStrict,
	})
}

// ResolveAssetBase resolves an assets base directory. An empty or unset
// environment variable selects defaultRelPath relative to projectRoot; an
// absolute override is used as-is; a relative override is resolved from
// projectRoot.
func ResolveAssetBase(projectRoot string, envVar string, defaultRelPath string) string {
	override := strings.TrimSpace(os.Getenv(envVar))
	switch {
	case override == "":
		return filepath.Join(projectRoot, filepath.FromSlash(defaultRelPath))
	case filepath.IsAbs(override):
		return filepath.Clean(override)
	default:
		return filepath.Join(projectRoot, override)
	}
}

// ResolveCRDPath joins the module's configured assets base with
// crdRelativePath. Exactly one of cfg.ChartDir or cfg.ManifestDir must be
// configured; any other combination is an error.
func ResolveCRDPath(projectRoot string, cfg modules.ModuleConfig, crdRelativePath string) (string, error) {
	chartBased := cfg.ChartDir != ""
	manifestBased := cfg.ManifestDir != ""

	var base, dir string

	switch {
	case chartBased && manifestBased:
		return "", fmt.Errorf(
			"module %s: exactly one of ChartDir or ManifestDir must be configured, both are set", cfg.Name)
	case !chartBased && !manifestBased:
		return "", fmt.Errorf(
			"module %s: one of ChartDir or ManifestDir must be configured, neither is set", cfg.Name)
	case chartBased:
		base = ResolveAssetBase(projectRoot, DefaultChartsPathEnv, defaultChartsRelPath)
		dir = cfg.ChartDir
	default:
		base = ResolveAssetBase(projectRoot, DefaultManifestsPathEnv, defaultManifestsRelPath)
		dir = cfg.ManifestDir
	}

	return filepath.Join(base, dir, filepath.FromSlash(crdRelativePath)), nil
}

// SelectModuleCRD reads the raw multi-document YAML at path and returns the
// single CRD matching cfg.GVK's group, kind, and served version. Non-CRD
// documents in the same file are ignored. Unreadable or malformed sources,
// zero or multiple matching CRDs, an empty schema, or a namespaced scope
// are errors with actionable diagnostics.
func SelectModuleCRD(cfg modules.ModuleConfig, path string) (*apiextensionsv1.CustomResourceDefinition, error) {
	guidance := fmt.Sprintf("prepare local module assets with 'make get-manifests' (expected %s)", cfg.GVK.String())

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("module %s: unable to read CRD file %s: %w; %s", cfg.Name, path, err, guidance)
	}

	s, err := crdScheme()
	if err != nil {
		return nil, err
	}

	objs, err := resources.Decode(serializer.NewCodecFactory(s).UniversalDeserializer(), data)
	if err != nil {
		return nil, fmt.Errorf("module %s: malformed CRD file %s: %w; %s", cfg.Name, path, err, guidance)
	}

	var (
		matchedCRD *apiextensionsv1.CustomResourceDefinition
		crdCount   int
		matchCount int
	)

	for i := range objs {
		if objs[i].GroupVersionKind() != gvk.CustomResourceDefinition {
			continue
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := resources.ObjectFromUnstructured(s, &objs[i], crd); err != nil {
			return nil, fmt.Errorf("module %s: malformed CustomResourceDefinition document in %s: %w; %s", cfg.Name, path, err, guidance)
		}
		crdCount++

		if !crdMatchesGVK(crd, cfg.GVK) {
			continue
		}
		matchCount++

		if matchCount > 1 {
			return nil, fmt.Errorf(
				"module %s: ambiguous CRD source %s: found %d documents matching %s, expected exactly one; %s",
				cfg.Name, path, matchCount, cfg.GVK.String(), guidance)
		}

		matchedCRD = crd
	}

	if matchedCRD == nil {
		return nil, fmt.Errorf(
			"module %s: no CRD matching %s found in %s (%d document(s), %d CRD(s)); %s",
			cfg.Name, cfg.GVK.String(), path, len(objs), crdCount, guidance)
	}

	if err := validateSelectedCRD(matchedCRD, cfg, path); err != nil {
		return nil, err
	}

	return matchedCRD, nil
}

// crdScheme builds the minimal scheme required to decode CRD documents.
func crdScheme() (*runtime.Scheme, error) {
	s := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(s); err != nil {
		return nil, fmt.Errorf("registering apiextensions types: %w", err)
	}

	return s, nil
}

// crdMatchesGVK reports whether crd serves gvk's group, kind, and version.
func crdMatchesGVK(crd *apiextensionsv1.CustomResourceDefinition, gvk schema.GroupVersionKind) bool {
	if crd.Spec.Group != gvk.Group || crd.Spec.Names.Kind != gvk.Kind {
		return false
	}

	for _, version := range crd.Spec.Versions {
		if version.Name == gvk.Version && version.Served {
			return true
		}
	}

	return false
}

// validateSelectedCRD enforces the nonempty-schema and cluster-scope
// requirements for module CRs on the selected CRD.
func validateSelectedCRD(crd *apiextensionsv1.CustomResourceDefinition, cfg modules.ModuleConfig, path string) error {
	guidance := fmt.Sprintf("prepare local module assets with 'make get-manifests' (expected %s)", cfg.GVK.String())

	for _, version := range crd.Spec.Versions {
		if version.Name != cfg.GVK.Version {
			continue
		}

		if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			return fmt.Errorf("module %s: CRD %s in %s: version %s has no schema; %s",
				cfg.Name, crd.Name, path, cfg.GVK.Version, guidance)
		}
	}

	if crd.Spec.Scope != apiextensionsv1.ClusterScoped {
		return fmt.Errorf("module %s: CRD %s in %s: scope is %q, module CRs must be cluster-scoped; %s",
			cfg.Name, crd.Name, path, crd.Spec.Scope, guidance)
	}

	return nil
}
