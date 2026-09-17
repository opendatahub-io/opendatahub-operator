package e2e_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/e2e/webhooks/conversion"

	. "github.com/onsi/gomega"
)

func conversionWebhookSuiteSelected() bool {
	if !testOpts.conversionWebhookTest || (!testOpts.conversionWebhookDSC && !testOpts.conversionWebhookPlatform) {
		return false
	}
	if testOpts.conversionWebhookExplicit {
		return true
	}
	return (TestTag(testOpts.tag) == All || TestTag(testOpts.tag) == Tier3) &&
		Components.enabled && Services.enabled &&
		!testOpts.componentSelectionExplicit && !testOpts.serviceSelectionExplicit
}

func envIsSet(name string) bool {
	_, present := os.LookupEnv(name)
	return present
}

func findOrCreateWebhookDSCI(ctx context.Context, cli client.Client, desired *dsciv2.DSCInitialization) (bool, error) {
	key := client.ObjectKeyFromObject(desired)
	existing := &dsciv2.DSCInitialization{}
	if err := cli.Get(ctx, key, existing); err == nil {
		return false, nil
	} else if !k8serr.IsNotFound(err) {
		return false, fmt.Errorf("get DSCInitialization %s: %w", key, err)
	}

	if err := cli.Create(ctx, desired); err != nil {
		if k8serr.IsAlreadyExists(err) {
			// Another actor created the singleton after our Get; it is not ours to delete.
			return false, nil
		}
		return false, fmt.Errorf("create DSCInitialization %s: %w", key, err)
	}

	return true, nil
}

func cleanupCreatedWebhookDSCI(t *testing.T, cli client.Client, created *dsciv2.DSCInitialization) {
	t.Helper()

	if created.UID == "" {
		t.Error("cannot safely clean up test-created DSCInitialization without its UID")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := cli.Delete(ctx, created, client.Preconditions{UID: &created.UID})
	if k8serr.IsNotFound(err) {
		return
	}
	if k8serr.IsConflict(err) {
		t.Logf("DSCInitialization %s was replaced; leaving the replacement untouched", created.Name)
		return
	}
	if err != nil {
		t.Errorf("delete test-created DSCInitialization %s: %v", created.Name, err)
		return
	}

	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		current := &dsciv2.DSCInitialization{}
		err := cli.Get(ctx, client.ObjectKeyFromObject(created), current)
		if k8serr.IsNotFound(err) {
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(current.UID).NotTo(Equal(created.UID), "test-created DSCInitialization should be deleted")
	}).WithContext(ctx).WithPolling(time.Second).Should(Succeed())
}

func webhookConversionTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)
	tc.SkipIfXKSCluster(t)

	crdNames := make([]string, 0, 2)
	if testOpts.conversionWebhookDSC {
		if !testOpts.backupAndRestoreDSCIandDSC &&
			(!testOpts.cleanUpPreviousResources || testOpts.deletionPolicy != DeletionPolicyAlways) {
			t.Fatal("DSC webhook conversion deletes default-dsc; enable DSCI/DSC backup and restore or use cleanup with deletion-policy=always")
		}

		dsci := CreateDSCI(tc.DSCInitializationNamespacedName.Name, tc.AppsNamespace, tc.MonitoringNamespace)
		created, err := findOrCreateWebhookDSCI(t.Context(), tc.Client(), dsci)
		require.NoError(t, err)
		if created {
			t.Logf("Created DSCInitialization %s for webhook conversion tests", dsci.Name)
			t.Cleanup(func() {
				cleanupCreatedWebhookDSCI(t, tc.Client(), dsci)
			})
		} else {
			t.Logf("Using existing DSCInitialization %s without modifying it", dsci.Name)
		}

		t.Logf("Waiting for DSCInitialization %s to become Ready", dsci.Name)
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
			WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
			WithEventuallyTimeout(2*time.Minute),
		)
		crdNames = append(crdNames, "datascienceclusters.datasciencecluster.opendatahub.io")
	}
	if testOpts.conversionWebhookPlatform {
		crdNames = append(crdNames, "platforms.config.opendatahub.io")
	}

	for _, name := range crdNames {
		t.Run("prerequisite "+name, func(t *testing.T) {
			g := NewWithT(t)
			g.Eventually(func(g Gomega) {
				crd := &apiextv1.CustomResourceDefinition{}
				g.Expect(tc.Client().Get(t.Context(), types.NamespacedName{Name: name}, crd)).To(Succeed())
				g.Expect(crd.Spec.Conversion).NotTo(BeNil())
				g.Expect(crd.Spec.Conversion.Strategy).To(Equal(apiextv1.WebhookConverter))
				g.Expect(crd.Spec.Conversion.Webhook).NotTo(BeNil())
				g.Expect(crd.Spec.Conversion.Webhook.ClientConfig).NotTo(BeNil())
				g.Expect(crd.Spec.Conversion.Webhook.ClientConfig.CABundle).NotTo(BeEmpty())
			}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
		})
	}
	if t.Failed() {
		t.FailNow()
	}

	conversion.Run(t, testOpts.conversionWebhookDSC, testOpts.conversionWebhookPlatform)
}
