package e2e_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

const (
	defaultCodeFlareComponentName = "default-codeflare"
	defaultServiceMeshName        = "default-servicemesh"
)

type CRDToCreate struct {
	GVK  schema.GroupVersionKind
	Name string
}

var removedCRDToCreate = []CRDToCreate{
	{GVK: gvk.ServiceMesh, Name: defaultServiceMeshName},
}

type DSCIUpgradeTestCtx struct {
	*TestContext
}

func dscInitializationDeprecatedCRDTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// DSCI tests rely on resources that don't exist in XKS/KinD.
	tc.SkipIfXKSCluster(t)

	skipUnless(t, Tier1)

	// Create an instance of test context.
	dsciUpgradeTestCtx := DSCIUpgradeTestCtx{
		TestContext: tc,
	}

	// Register cleanup before creation so it runs even if createCRD() fails midway
	t.Cleanup(func() {
		for _, crd := range removedCRDToCreate {
			tc.DeleteResource(
				WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: strings.ToLower(crd.GVK.Kind) + "s." + crd.GVK.Group}),
				WithIgnoreNotFound(true),
			)
		}
	})

	dsciUpgradeTestCtx.createCRD(removedCRDToCreate)

	// Define test cases.
	testCases := []TestCase{
		{"servicemesh resource preserved after support removal", dsciUpgradeTestCtx.ValidateServiceMeshResourcePreservation},
		// RHOAIENG-48054: DSCI should stay Ready after suite scenarios (startup cleanup before default CR creation).
		{"default DSCInitialization remains Ready after deprecated CRD scenario", dsciUpgradeTestCtx.ValidateDefaultDSCIRemainsReadyAfterDeprecatedCRDScenarios},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// createCRD creates a mock CRD for the given component GVK if it doesn't already exist in the cluster.
func (tc *DSCIUpgradeTestCtx) createCRD(crdsToCreate []CRDToCreate) {
	for _, crd := range crdsToCreate {
		// Create mock CRD for the component
		mockCRD := mocks.NewMockCRD(crd.GVK.Group, crd.GVK.Version, crd.GVK.Kind, crd.Name)

		tc.EventuallyResourceCreated(
			WithObjectToCreate(mockCRD),
			WithAcceptableErr(k8serr.IsAlreadyExists, "IsAlreadyExists"),
			WithCustomErrorMsg("Failed to create CRD for %s component", crd.GVK.Kind),
			WithEventuallyTimeout(tc.TestTimeouts.shortEventuallyTimeout),
		)
	}
}

func (tc *DSCIUpgradeTestCtx) ValidateServiceMeshResourcePreservation(t *testing.T) {
	t.Helper()

	nn := types.NamespacedName{
		Name: defaultServiceMeshName,
	}

	dsci := tc.FetchDSCInitialization()

	tc.createOperatorManagedServiceMesh(defaultServiceMeshName, dsci)

	// Register cleanup at creation time - runs even on test failure/timeout
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.ServiceMesh, nn),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(true),
		)
	})

	tc.triggerDSCIReconciliation(t)

	// verify ServiceMesh still exists after reconciliation
	tc.EnsureResourceExistsConsistently(WithMinimalObject(gvk.ServiceMesh, nn),
		WithCustomErrorMsg("ServiceMesh service resource '%s' was expected to exist but was not found", defaultServiceMeshName),
	)
}

func (tc *DSCIUpgradeTestCtx) createOperatorManagedServiceMesh(serviceMeshName string, dsci *dsciv2.DSCInitialization) {
	existingServiceMesh := resources.GvkToUnstructured(gvk.ServiceMesh)
	existingServiceMesh.SetName(serviceMeshName)

	resources.SetLabels(existingServiceMesh, map[string]string{
		labels.PlatformPartOf: strings.ToLower(gvk.DSCInitialization.Kind),
	})

	resources.SetAnnotations(existingServiceMesh, map[string]string{
		odhAnnotations.ManagedByODHOperator: "true",
		odhAnnotations.PlatformVersion:      dsci.Status.Release.Version.String(),
		odhAnnotations.PlatformType:         string(dsci.Status.Release.Name),
		odhAnnotations.InstanceGeneration:   strconv.Itoa(int(dsci.GetGeneration())),
		odhAnnotations.InstanceUID:          string(dsci.GetUID()),
	})

	err := controllerutil.SetOwnerReference(dsci, existingServiceMesh, tc.Scheme())
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Failed to set owner reference from DSCInitialization '%s' to ServiceMesh service '%s'",
		dsci.GetName(), serviceMeshName)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(existingServiceMesh),
		WithCustomErrorMsg("Failed to create existing ServiceMesh service for preservation test"),
	)
}

func (tc *DSCIUpgradeTestCtx) ValidateDefaultDSCIRemainsReadyAfterDeprecatedCRDScenarios(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("DSCI should remain Ready after deprecated ServiceMesh CRD scenario (RHOAIENG-48054)"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)
}

func (tc *DSCIUpgradeTestCtx) triggerDSCIReconciliation(t *testing.T) {
	t.Helper()

	validPEM := generateTestCertPEM(t)

	// trigger DSCI reconciliation by setting a customCABundle with a valid PEM
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(setCustomCABundle(validPEM)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to trigger DSCI reconciliation"),
	)

	// restore original customCABundle in DSCInitialization instance
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(setCustomCABundle("")),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to trigger DSCI reconciliation"),
	)
}
