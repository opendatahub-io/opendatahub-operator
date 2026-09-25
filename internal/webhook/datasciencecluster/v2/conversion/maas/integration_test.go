package maas_test

import (
	"context"
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	dsciv1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	dsciv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const maasV2StateAnnotation = "conversion.opendatahub.io/maas-v2-state"

var maasKey = types.NamespacedName{Name: "maas-compatibility"}

func createDSCI(g Gomega, ctx context.Context, cli client.Client) {
	dsci := envtestutil.NewDSCI("dsci-for-dsc")
	g.Expect(cli.Create(ctx, dsci)).To(Succeed())
	g.Eventually(func() error {
		return cli.Get(ctx, client.ObjectKeyFromObject(dsci), dsci)
	}, "10s", "1s").Should(Succeed(), "DSCI object should be available after creation")
}

type maasTest struct {
	cli client.Client
}

func (m maasTest) withTest(t *testing.T, ctx context.Context) *testf.WithT {
	t.Helper()

	g := NewWithT(t)
	testContext, err := testf.NewTestContext(
		testf.WithContext(ctx),
		testf.WithClient(m.cli),
	)
	g.Expect(err).NotTo(HaveOccurred())

	return testContext.NewWithT(t)
}

func TestMaaSVersionedAdmission(t *testing.T) {
	g := NewWithT(t)

	ctx, env, teardown := envtestutil.SetupEnvAndClient(t,
		[]envt.RegisterWebhooksFn{
			v2webhook.RegisterWebhooks, v3webhook.RegisterWebhooks,
			dsciv1webhook.RegisterWebhooks, dsciv2webhook.RegisterWebhooks,
		}, nil, envtestutil.DefaultWebhookTimeout)
	t.Cleanup(teardown)

	g.Expect(env.ConfigureCRDConversion(ctx, "datascienceclusters.datasciencecluster.opendatahub.io")).To(Succeed())
	createDSCI(g, ctx, env.Client())

	warnings := testf.NewWarningRecorder()
	testContext, err := testf.NewTestContext(
		testf.WithContext(ctx),
		testf.WithRestConfig(env.Config()),
		testf.WithScheme(env.Scheme()),
		testf.WithWarningHandler(warnings),
	)
	g.Expect(err).NotTo(HaveOccurred())
	tests := maasTest{cli: testContext.Client()}

	// One environment is shared; each sequential test cleans up the singleton.
	t.Run("v3 storage omits legacy", tests.testMaaSStorage)
	t.Run("v2 round trip and CEL", tests.testMaaSRoundTrip)
	t.Run("KServe parent transition enables legacy MaaS", tests.testMaaSParentTransition)
	t.Run("unrelated updates preserve marker", tests.testMaaSUnrelatedUpdates)
	t.Run("parent edits preserve marker", tests.testMaaSParentEdits)
	t.Run("canonical edits retire marker", tests.testMaaSCanonicalEdits)
	t.Run("v2 to v3 conversion", tests.testMaaSConversionV2ToV3)
	t.Run("v3 to v2 conversion", tests.testMaaSConversionV3ToV2)
	t.Run("native create strips marker", tests.testMaaSNativeCreate)

	messages := warnings.Messages()
	g.Expect(messages).To(ContainElement(
		"datasciencecluster.opendatahub.io/v2 DataScienceCluster is deprecated; use datasciencecluster.opendatahub.io/v3 DataScienceCluster"),
	)
	g.Expect(messages).NotTo(ContainElement(
		ContainSubstring("modelsAsService is deprecated")),
	)
}

// testMaaSStorage verifies that the deprecated v2 KServe MaaS field is not persisted in v3 storage.
func (m maasTest) testMaaSStorage(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	wt := m.withTest(t, ctx)
	createMaaS(t, ctx, m.cli)

	// Storage keeps only the canonical v3 MaaS field; the deprecated v2 field is omitted.
	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		Not(jq.Match(`.spec.components.kserve.modelsAsService != null`)))
}

// testMaaSRoundTrip verifies MaaS provenance across v2/v3 reads, updates, and CEL validation.
func (m maasTest) testMaaSRoundTrip(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	g := NewWithT(t)
	wt := m.withTest(t, ctx)

	createMaaS(t, ctx, m.cli)

	// A v2-created object is stored in v3 with canonical MaaS enabled and provenance recorded.
	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(And(
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation),
	))

	// Reading through v2 restores legacy MaaS authority and retains the migrated parent.
	wt.Get(gvk.DataScienceClusterV2, maasKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`),
	))

	// A canonical v3 disable retires the legacy state before testing the CEL guard.
	wt.Update(
		gvk.DataScienceClusterV3,
		maasKey,
		jq.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Removed"`),
	).Should(Succeed(), "canonical MaaS can be disabled explicitly")

	wt.Get(gvk.DataScienceClusterV2, maasKey).Should(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`))

	// CEL prevents a legacy MaaS state that was removed from being re-enabled.
	_, err := wt.Update(gvk.DataScienceClusterV2, maasKey, testf.And(
		jq.Transform(`.spec.components.kserve.modelsAsService.managementState = "Managed"`),
	)).Get()

	g.Expect(err).To(And(
		Satisfy(k8serr.IsInvalid),
		MatchError(ContainSubstring("modelsAsService")),
	))

	wt.Update(
		gvk.DataScienceClusterV2,
		maasKey,
		jq.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Removed"`),
	).Should(Succeed(), "canonical MaaS can be disabled explicitly")

	// Canonical v3 MaaS can still be disabled explicitly after the legacy field is removed.
	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`))
}

// testMaaSParentTransition verifies that enabling the KServe parent makes
// legacy Managed MaaS effective and records its v2 provenance in v3.
func (m maasTest) testMaaSParentTransition(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	wt := m.withTest(t, ctx)
	createMaaSWithParent(t, ctx, m.cli, operatorv1.Removed, "Managed", operatorv1.Removed)

	// The legacy intent remains marked while the Removed KServe parent gates it off.
	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(And(
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation),
	))

	// Enabling the KServe parent makes the legacy Managed state effective on conversion.
	wt.Update(
		gvk.DataScienceClusterV2,
		maasKey,
		jq.Transform(`.spec.components.kserve.managementState = "Managed"`),
	).Should(Succeed())

	// V3 exposes enabled canonical MaaS, synthesizes the enabled AI Gateway parent,
	// and keeps the marker so a v2 read can restore legacy Managed.
	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation),
	))

	// Disabling KServe through v2 gates MaaS off without disabling the migrated AI Gateway parent.
	wt.Update(
		gvk.DataScienceClusterV2,
		maasKey,
		jq.Transform(`.spec.components.kserve.managementState = "Removed"`),
	).Should(Succeed())

	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(And(
		jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
		jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
		jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation),
	))
}

// testMaaSUnrelatedUpdates verifies that unrelated spec, metadata, and status updates preserve provenance.
func (m maasTest) testMaaSUnrelatedUpdates(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	wt := m.withTest(t, ctx)
	createMaaS(t, ctx, m.cli)
	v3 := wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation),
	)

	marker := resources.GetAnnotation(v3, maasV2StateAnnotation)

	for _, mutation := range []struct {
		name  string
		apply func(*unstructured.Unstructured) error
	}{
		{"spec", jq.Transform(`.spec.components.dashboard.managementState = "Managed"`)},
		{"metadata", matchers.SetLabel("preserved", "yes")},
		{"delete annotation", matchers.RemoveAnnotation(maasV2StateAnnotation)},
		{"resubmit marker", matchers.SetAnnotation(maasV2StateAnnotation, "legacy-managed")},
	} {
		wt.Update(gvk.DataScienceClusterV3, maasKey, mutation.apply).Should(Succeed(), mutation.name)
		wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
			jq.Match(`.metadata.annotations."%s" == "%s"`, maasV2StateAnnotation, marker), mutation.name)
		wt.Get(gvk.DataScienceClusterV2, maasKey).Should(
			jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`))
	}

	for _, update := range []struct {
		name      string
		objectGVK schema.GroupVersionKind
	}{
		{name: "v3 status update preserves marker", objectGVK: gvk.DataScienceClusterV3},
		{name: "v2 status update preserves marker", objectGVK: gvk.DataScienceClusterV2},
	} {
		t.Run(update.name, func(t *testing.T) {
			wt := m.withTest(t, ctx)
			wt.UpdateStatus(
				update.objectGVK,
				maasKey,
				jq.Transform(`.status.components.modelsAsAService.managementState = "Managed"`),
			).Should(Succeed())

			wt.Get(gvk.DataScienceClusterV3, maasKey).Should(And(
				jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
				jq.Match(`.metadata.annotations."%s" == "%s"`, maasV2StateAnnotation, marker),
			))
			wt.Get(gvk.DataScienceClusterV2, maasKey).Should(And(
				jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
				jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`),
			))
		})
	}
}

// testMaaSParentEdits verifies that changing the KServe or AIGateway parent state does not change MaaS provenance.
func (m maasTest) testMaaSParentEdits(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	wt := m.withTest(t, ctx)
	createMaaS(t, ctx, m.cli)
	v3 := wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation))
	marker := resources.GetAnnotation(v3, maasV2StateAnnotation)

	for _, edit := range []struct {
		name string
		path string
	}{
		{name: "KServe parent", path: ".spec.components.kserve.managementState"},
		{name: "AI Gateway parent", path: ".spec.components.aigateway.managementState"},
	} {
		t.Run(edit.name, func(t *testing.T) {
			wt.Update(
				gvk.DataScienceClusterV3,
				maasKey,
				jq.Transform(`%s = "Removed"`, edit.path),
			).Should(Succeed())

			wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
				jq.Match(`.metadata.annotations."%s" == "%s"`, maasV2StateAnnotation, marker))
			wt.Get(gvk.DataScienceClusterV2, maasKey).Should(
				jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Managed"`))
		})
	}
}

// testMaaSCanonicalEdits verifies that explicitly disabling canonical MaaS retires legacy provenance.
func (m maasTest) testMaaSCanonicalEdits(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	wt := m.withTest(t, ctx)
	createMaaS(t, ctx, m.cli)
	v3 := wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation))
	marker := resources.GetAnnotation(v3, maasV2StateAnnotation)

	// Disabling canonical MaaS retires the legacy provenance marker.
	wt.Update(
		gvk.DataScienceClusterV3,
		maasKey,
		jq.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Removed"`),
	).Should(Succeed())

	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		Not(jq.Match(`.metadata.annotations | has("%s")`, maasV2StateAnnotation)))
	wt.Get(gvk.DataScienceClusterV2, maasKey).Should(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`))

	// Re-submitting a stale marker must not restore provenance or legacy MaaS.
	wt.Update(gvk.DataScienceClusterV3, maasKey, testf.And(
		jq.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Removed"`),
		matchers.SetAnnotation(maasV2StateAnnotation, marker),
	)).Should(Succeed())

	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		Not(jq.Match(`.metadata.annotations | has("%s")`, maasV2StateAnnotation)))
	wt.Get(gvk.DataScienceClusterV2, maasKey).Should(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`))
}

type maasStateSelectionCase struct {
	name          string
	canonical     string
	kserveParent  string
	legacy        string
	wantCanonical string
	wantLegacy    string
	wantMarker    bool
}

func maasStateSelectionCases() []maasStateSelectionCase {
	return []maasStateSelectionCase{
		{
			name:          "legacy Managed is selected with Managed KServe",
			canonical:     "Removed",
			kserveParent:  "Managed",
			legacy:        "Managed",
			wantCanonical: "Managed",
			wantLegacy:    "Managed",
			wantMarker:    true,
		},
		{
			name:          "canonical Managed wins without legacy provenance",
			canonical:     "Managed",
			kserveParent:  "Managed",
			legacy:        "Removed",
			wantCanonical: "Managed",
			wantLegacy:    "Removed",
		},
		{
			name:          "both Removed remain disabled",
			canonical:     "Removed",
			kserveParent:  "Managed",
			legacy:        "Removed",
			wantCanonical: "Removed",
			wantLegacy:    "Removed",
		},
		{
			name:          "canonical Managed replaces legacy Managed",
			canonical:     "Managed",
			kserveParent:  "Managed",
			legacy:        "Managed",
			wantCanonical: "Managed",
			wantLegacy:    "Removed",
		},
		{
			name:          "legacy Managed is gated by Removed KServe",
			canonical:     "Removed",
			kserveParent:  "Removed",
			legacy:        "Managed",
			wantCanonical: "Removed",
			wantLegacy:    "Managed",
			wantMarker:    true,
		},
		{
			name:          "empty KServe parent is gated like Removed",
			canonical:     "Removed",
			kserveParent:  "",
			legacy:        "Managed",
			wantCanonical: "Removed",
			wantLegacy:    "Managed",
			wantMarker:    true,
		},
	}
}

// testMaaSConversionV2ToV3 verifies canonical state selection and provenance when a v2 object is read as v3.
func (m maasTest) testMaaSConversionV2ToV3(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	for _, tc := range maasStateSelectionCases() {
		name := fmt.Sprintf("selection: %s", tc.name)
		t.Run(name, func(t *testing.T) {
			wt := m.withTest(t, ctx)
			createMaaSWithParent(
				t,
				ctx,
				m.cli,
				operatorv1.ManagementState(tc.canonical),
				tc.legacy,
				operatorv1.ManagementState(tc.kserveParent),
			)
			canonicalMatcher := jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "%s"`, tc.wantCanonical)
			if tc.wantMarker {
				wt.Get(gvk.DataScienceClusterV3, maasKey).Should(And(
					canonicalMatcher,
					jq.Match(`.metadata.annotations."%s" == "legacy-managed"`, maasV2StateAnnotation),
				))
			} else {
				wt.Get(gvk.DataScienceClusterV3, maasKey).Should(And(
					canonicalMatcher,
					Not(jq.Match(`.metadata.annotations | has("%s")`, maasV2StateAnnotation)),
				))
			}
		})
	}
}

// testMaaSConversionV3ToV2 verifies canonical and legacy state projection when a v3 object is read as v2.
func (m maasTest) testMaaSConversionV3ToV2(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	for _, tc := range maasStateSelectionCases() {
		name := fmt.Sprintf("selection: %s", tc.name)
		t.Run(name, func(t *testing.T) {
			wt := m.withTest(t, ctx)
			createMaaSWithParent(
				t,
				ctx,
				m.cli,
				operatorv1.ManagementState(tc.canonical),
				tc.legacy,
				operatorv1.ManagementState(tc.kserveParent),
			)

			// Read v3 first so the assertion covers the conversion that stores the object.
			wt.Get(gvk.DataScienceClusterV3, maasKey).Should(Not(BeNil()))
			wt.Get(gvk.DataScienceClusterV2, maasKey).Should(And(
				jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "%s"`, tc.canonical),
				jq.Match(`.spec.components.kserve.modelsAsService.managementState == "%s"`, tc.wantLegacy),
			))
		})
	}
}

// testMaaSNativeCreate verifies that a v3-native create cannot carry v2 conversion provenance.
func (m maasTest) testMaaSNativeCreate(t *testing.T) {
	t.Helper()
	ctx := t.Context()

	g := NewWithT(t)
	wt := m.withTest(t, ctx)

	// Capture a valid annotation from a real conversion, then try to reuse it on create.
	legacy := newLegacyMaaS()
	g.Expect(m.cli.Create(ctx, legacy)).To(Succeed())
	v3 := wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		jq.Match(`.metadata.annotations | has("%s")`, maasV2StateAnnotation))
	wt.Delete(gvk.DataScienceClusterV2, maasKey).Eventually().Should(
		MatchError(k8serr.IsNotFound, "IsNotFound"))

	v3.SetResourceVersion("")
	v3.SetUID("")
	v3.SetCreationTimestamp(metav1.Time{})
	v3.SetManagedFields(nil)
	g.Expect(m.cli.Create(ctx, v3)).To(Succeed())
	t.Cleanup(func() { g.Expect(m.cli.Delete(context.Background(), v3)).To(Succeed()) })

	wt.Get(gvk.DataScienceClusterV3, maasKey).Should(
		Not(jq.Match(`.metadata.annotations | has("%s")`, maasV2StateAnnotation)))
	wt.Get(gvk.DataScienceClusterV2, maasKey).Should(
		jq.Match(`.spec.components.kserve.modelsAsService.managementState == "Removed"`))
}

func createMaaS(t *testing.T, ctx context.Context, cli client.Client) {
	t.Helper()

	createMaaSWithStates(t, ctx, cli, "", "Managed")
}

func createMaaSWithStates(t *testing.T, ctx context.Context, cli client.Client, canonical operatorv1.ManagementState, legacy string) {
	t.Helper()

	createMaaSWithParent(t, ctx, cli, canonical, legacy, operatorv1.Managed)
}

func createMaaSWithParent(
	t *testing.T,
	ctx context.Context,
	cli client.Client,
	canonical operatorv1.ManagementState,
	legacy string,
	kserveParent operatorv1.ManagementState,
) {
	t.Helper()

	g := NewWithT(t)
	dsc := newLegacyMaaS()
	dsc.Spec.Components.Kserve.ManagementState = kserveParent
	dsc.Spec.Components.Kserve.ModelsAsService.ManagementState = operatorv1.ManagementState(legacy)
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = canonical

	g.Expect(cli.Create(ctx, dsc)).To(Succeed())

	// Cleanup must continue even if the test context has been cancelled.
	//nolint:contextcheck
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		g.Eventually(func() error {
			return cli.Delete(cleanupCtx, dsc)
		}, "10s", "100ms").Should(MatchError(k8serr.IsNotFound, "IsNotFound"))
	})
}

func newLegacyMaaS() *dscv2.DataScienceCluster {
	dsc := &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.DataScienceClusterV2.GroupVersion().String(),
			Kind:       gvk.DataScienceClusterV2.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: "maas-compatibility"},
	}
	dsc.Spec.Components.Kserve.ManagementState = operatorv1.Managed
	dsc.Spec.Components.Kserve.ModelsAsService.ManagementState = operatorv1.Managed
	dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = operatorv1.Removed

	return dsc
}
