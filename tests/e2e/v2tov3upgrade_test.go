package e2e_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	defaultCodeFlareComponentName = "default-codeflare"
)

type V2Tov3UpgradeTestCtx struct {
	*TestContext
}

func v2Tov3UpgradeTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	v2Tov3UpgradeTestCtx := V2Tov3UpgradeTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{"codeflare present in the cluster before upgrade, after upgrade not removed", v2Tov3UpgradeTestCtx.ValidateCodeFlareSupportRemovalNotRemoveComponent},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateCodeFlareSupportRemovalNotRemoveComponent(t *testing.T) {
	t.Helper()

	nn := types.NamespacedName{
		Name: defaultCodeFlareComponentName,
	}

	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.CodeFlare, nn),
			WithIgnoreNotFound(true),
			WithCustomErrorMsg("Failed to delete CodeFlare component resource '%s'", defaultCodeFlareComponentName),
		)
	})

	dsc := tc.FetchDataScienceCluster()

	dscOwnerReference := metav1.OwnerReference{
		APIVersion:         gvk.DataScienceCluster.GroupVersion().String(),
		Kind:               gvk.DataScienceCluster.Kind,
		Name:               dsc.GetName(),
		UID:                dsc.GetUID(),
		BlockOwnerDeletion: ptr.To(true),
		Controller:         ptr.To(true),
	}
	marshalledOwnerReference, err := json.Marshal(dscOwnerReference)
	require.NoError(t, err)

	dsci := tc.FetchDSCInitialization()

	tc.EnsureResourceCreatedOrPatched(
		WithMinimalObject(gvk.CodeFlare, nn),
		WithMutateFunc(testf.Transform(`
		.metadata.ownerReferences = [%s] |
		.metadata.labels["%s"] = "%s" |
		.metadata.annotations["%s"] = "%s" |
		.metadata.annotations["%s"] = "%s" |
		.metadata.annotations["%s"] = "%s" |
		.metadata.annotations["%s"] = "%s"`,
			marshalledOwnerReference,
			labels.PlatformPartOf,
			strings.ToLower(gvk.DataScienceCluster.Kind),
			odhAnnotations.PlatformVersion,
			dsci.Status.Release.Version.String(),
			odhAnnotations.PlatformType,
			string(dsci.Status.Release.Name),
			odhAnnotations.InstanceGeneration,
			strconv.Itoa(int(dsc.GetGeneration())),
			odhAnnotations.InstanceUID,
			string(dsc.GetUID()),
		)),
		WithCustomErrorMsg("Failed to create or update CodeFlare component resource '%s'", defaultCodeFlareComponentName),
	)

	tc.triggerDSCReconciliation(t, "kueue")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CodeFlare, nn),
		WithCustomErrorMsg("CodeFlare component resource '%s' was expected to exist but was not found", defaultCodeFlareComponentName),
		WithEventuallyTimeout(10*time.Second),
	)
}

func (tc *V2Tov3UpgradeTestCtx) triggerDSCReconciliation(t *testing.T, componentToEnable string) {
	t.Helper()

	t.Cleanup(func() {
		tc.EventuallyResourceCreatedOrUpdated(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentToEnable, operatorv1.Removed)),
		)
	})

	// This is needed to trigger another DSC reconciliation
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.kueue.managementState = "%s"`, operatorv1.Managed)),
		WithCustomErrorMsg("Failed to update DSC resource '%s'", tc.DataScienceClusterNamespacedName.Name),
		WithEventuallyTimeout(10*time.Second),
	)

	tc.g.Eventually(
		func(g Gomega) {
			dscAfterUpdate := tc.FetchDataScienceCluster()
			status := dscAfterUpdate.GetStatus()

			// Check that the DataScienceCluster has been reconciled by verifying observedGeneration
			currentGeneration := dscAfterUpdate.GetGeneration()
			observedGeneration := status.ObservedGeneration
			g.Expect(currentGeneration).To(Equal(observedGeneration),
				"DataScienceCluster '%s' should have been reconciled (observedGeneration should match generation)",
				tc.DataScienceClusterNamespacedName.Name,
				componentToEnable,
			)
		},
	).WithTimeout(tc.TestTimeouts.mediumEventuallyTimeout).Should(Succeed())
}
