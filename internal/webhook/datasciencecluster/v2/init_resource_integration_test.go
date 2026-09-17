package v2_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	dsciv3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	dsciv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

const dscSampleRelPath = "config/rhoai/samples/datasciencecluster_v3_datasciencecluster.yaml"

// componentStates extracts the top-level managementState of every supported component
// in a DataScienceCluster v3 Components tree. These fields come from common.ManagementSpec,
// which -- unlike nested fields such as kserve.nim -- carries NO +kubebuilder:default,
// so an unset one round-trips as "" (not Managed/Removed). Comparing this map before
// and after an API-server round-trip proves the shipped default DSC specifies every
// supported component explicitly.
//
// Deprecated components are intentionally excluded: the shipped default must not present
// them to users (RHOAIENG-89419). llamastackoperator (renamed to ogx) is dropped from the
// default entirely; kserve.modelsAsService (migrated to aigateway.modelsAsAService) is a
// nested field and is asserted absent from the sample separately. See TestDefaultDSCFromSampleAdmittedByAPIServer.
func componentStates(c dscv3.Components) map[string]operatorv1.ManagementState {
	return map[string]operatorv1.ManagementState{
		"aigateway":            c.AIGateway.ManagementState,
		"dashboard.standard":   c.Dashboard.Standard.ManagementState,
		"aipipelines":          c.AIPipelines.ManagementState,
		"kserve":               c.Kserve.ManagementState,
		"kueue":                c.Kueue.ManagementState,
		"trainer":              c.Trainer.ManagementState,
		"ray":                  c.Ray.ManagementState,
		"workbenches":          c.Workbenches.ManagementState,
		"trustyai":             c.TrustyAI.ManagementState,
		"aiHub":                c.AIHub.ManagementState,
		"data.featureStore":    c.Data.FeatureStore.ManagementState,
		"ogx":                  c.OGX.ManagementState,
		"mlflowoperator":       c.MLflowOperator.ManagementState,
		"sparkoperator":        c.SparkOperator.ManagementState,
		"mcplifecycleoperator": c.MCPLifecycleOperator.ManagementState,
	}
}

// TestDefaultDSCFromSampleAdmittedByAPIServer is the RHOAIENG-89419 integration guard (I1).
//
// U1 (cmd/manifest-tools .../init_resource_sync_test.go) proves the CSV
// initialization-resource annotation is textually identical to this sample, and U2
// proves the annotation decodes into the v3 API type. Neither runs the object through a
// real API server, so neither proves the shipped default DSC is actually *admissible*:
// that it survives CRD structural + CEL validation and the validating/defaulting webhooks
// a user's create request hits -- via either OLM path ("DataScienceCluster required"
// prompt from the annotation, or the "Provided APIs" tab from alm-examples/the sample).
//
// This test decodes the shipped sample and creates it on an envtest API server with the
// DSC v3 webhooks registered, asserting:
//  1. Create succeeds (admissibility);
//  2. every top-level component managementState round-trips unchanged and non-empty
//     (a complete default, since these fields have no schema default);
//  3. the defaulting webhook is a no-op on the already-complete sample (it neither
//     overwrites the explicit AIHub.RegistriesNamespace nor changes Kserve.NIM);
//  4. deprecated components are absent from the shipped default, so users are never
//     presented with a config they must remediate (RHOAIENG-89419): llamastackoperator
//     (renamed to ogx) and kserve.modelsAsService (migrated to aigateway.modelsAsAService).
//
// A two-path (annotation vs sample) comparison would be redundant: U1 proves the two
// inputs are identical and API-server defaulting is deterministic, so testing one path
// covers both.
func TestDefaultDSCFromSampleAdmittedByAPIServer(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	ctx, env, teardown := envtestutil.SetupEnvAndClient(
		t,
		[]envt.RegisterWebhooksFn{
			v3webhook.RegisterWebhooks,
			dsciv3webhook.RegisterWebhooks,
			v2webhook.RegisterWebhooks,
			dsciv2webhook.RegisterWebhooks,
		},
		[]envt.RegisterControllersFn{},
		envtestutil.DefaultWebhookTimeout,
	)
	t.Cleanup(teardown)

	// A DSCI is the established precondition for DSC tests in this package.
	createDSCI(g, ctx, env.Client())

	sampleBytes, err := env.ReadFile(dscSampleRelPath)
	g.Expect(err).NotTo(HaveOccurred(), "reading %s", dscSampleRelPath)

	dsc := &dscv3.DataScienceCluster{}
	g.Expect(yaml.Unmarshal(sampleBytes, dsc)).To(Succeed(), "decoding %s into a v3 DataScienceCluster", dscSampleRelPath)

	// (4) Deprecated components must not appear in the shipped default. Checked on the raw
	// decoded sample, so decoding into the v3 type cannot silently discard the removed
	// kserve.modelsAsService field. The sample feeds
	// alm-examples and -- per U1 -- is textually identical to the initialization-resource
	// annotation, so this guards both OLM install paths against re-introducing deprecated
	// items into the default users are presented with (RHOAIENG-89419).
	var rawSample map[string]any
	g.Expect(yaml.Unmarshal(sampleBytes, &rawSample)).To(Succeed())
	for _, field := range []string{"trainingoperator", "llamastackoperator"} {
		_, legacyPresent, err := unstructured.NestedFieldNoCopy(rawSample, "spec", "components", field)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(legacyPresent).To(BeFalse(), "%s is deprecated and must not be set in the shipped default DSC", field)
	}
	_, legacyPresent, err := unstructured.NestedFieldNoCopy(rawSample, "spec", "components", "kserve", "modelsAsService")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(legacyPresent).To(BeFalse(),
		"kserve.modelsAsService is deprecated (migrated to aigateway.modelsAsAService) and must not be set in the shipped default DSC")

	// Capture the intended states before create -- controller-runtime's Create writes the
	// server response (including defaults) back into dsc, so read expectations first.
	expectedStates := componentStates(dsc.Spec.Components)
	expectedApplicationNamespace := dsc.Spec.Components.AIHub.ApplicationNamespace

	// (1) Admissibility: the shipped default DSC must be accepted by the live API server.
	g.Expect(env.Client().Create(ctx, dsc)).To(Succeed(),
		"the shipped default DSC (%s) must be admissible: it must pass CRD structural + CEL "+
			"validation and the validating webhook, since OLM creates exactly this object "+
			"from the initialization-resource annotation (RHOAIENG-89419)", dscSampleRelPath)

	fetched := &dscv3.DataScienceCluster{}
	g.Eventually(func() error {
		return env.Client().Get(ctx, client.ObjectKey{Name: dsc.Name}, fetched)
	}, "10s", "1s").Should(Succeed(), "the created default DSC should be retrievable")

	// (2) Every top-level component managementState round-trips unchanged and non-empty.
	fetchedStates := componentStates(fetched.Spec.Components)
	for component, want := range expectedStates {
		g.Expect(want).NotTo(BeEmpty(),
			"the shipped default DSC leaves component %q with an empty managementState; top-level "+
				"managementState has no schema default, so users would get an incomplete default DSC "+
				"(RHOAIENG-89419)", component)
		g.Expect(fetchedStates[component]).To(Equal(want),
			"component %q managementState changed across the API-server round-trip", component)
	}

	// (3) The defaulting webhook must not disturb the already-complete sample.
	g.Expect(fetched.Spec.Components.AIHub.ApplicationNamespace).To(Equal(expectedApplicationNamespace),
		"defaulting must preserve the explicit AIHub.applicationNamespace from the sample")
	g.Expect(fetched.Spec.Components.Kserve.NIM.ManagementState).To(Equal(operatorv1.Managed),
		"Kserve.NIM should remain Managed (the sample sets it, and the webhook only defaults it when empty)")
}
