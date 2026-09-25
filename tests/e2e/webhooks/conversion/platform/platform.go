package platform

import (
	"context"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	configv1alpha2 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

const platformConversionE2EAnnotation = "e2e.opendatahub.io/platform-conversion"

type PlatformSuite struct {
	testContext *testf.TestContext
}

func Run(t *testing.T, testContext *testf.TestContext) {
	t.Helper()
	m := PlatformSuite{testContext: testContext}
	t.Run("v1alpha1_v1alpha2", m.v1alpha1ToV1alpha2)
}

// v1alpha1ToV1alpha2 writes through both served API versions and reads through
// the other version, exercising the installed CRD conversion webhook and the
// v1alpha2 storage representation on the cluster.
func (m PlatformSuite) v1alpha1ToV1alpha2(t *testing.T) {
	g := NewWithT(t)
	ctx := m.testContext.Context()
	cli := m.testContext.Client()
	key := types.NamespacedName{Name: configv1alpha2.PlatformInstanceName}

	original := &configv1alpha2.Platform{}
	err := cli.Get(ctx, key, original)
	createdForTest := false
	if k8serr.IsNotFound(err) {
		original = &configv1alpha2.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: key.Name},
		}
		if err := cli.Create(ctx, original); err != nil {
			t.Fatalf("create Platform for conversion E2E: %v", err)
		}
		createdForTest = true
	} else if err != nil {
		t.Fatalf("get Platform before conversion E2E: %v", err)
	}

	original = original.DeepCopy()
	originalSpec := original.Spec
	originalAnnotation, hadOriginalAnnotation := original.Annotations[platformConversionE2EAnnotation]
	t.Cleanup(func() {
		if createdForTest {
			if err := cli.Delete(context.Background(), &configv1alpha2.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: key.Name},
			}); err != nil && !k8serr.IsNotFound(err) {
				t.Errorf("delete test Platform after conversion E2E: %v", err)
			}
			return
		}

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &configv1alpha2.Platform{}
			if err := cli.Get(context.Background(), key, latest); err != nil {
				return err
			}
			latest.Spec.Modules.AIHub = originalSpec.Modules.AIHub
			latest.Spec.Modules.Data = originalSpec.Modules.Data
			if hadOriginalAnnotation {
				if latest.Annotations == nil {
					latest.Annotations = make(map[string]string)
				}
				latest.Annotations[platformConversionE2EAnnotation] = originalAnnotation
			} else {
				delete(latest.Annotations, platformConversionE2EAnnotation)
			}
			return cli.Update(context.Background(), latest)
		})
		if err != nil {
			t.Errorf("restore Platform after conversion E2E: %v", err)
		}
	})

	// Keep each module's effective state unchanged during the test. Empty has
	// the same effective behavior as Removed, so normalize only empty values.
	aiHubState := effectivePlatformState(original.Spec.Modules.AIHub.ManagementState)
	dataState := effectivePlatformState(original.Spec.Modules.Data.ManagementState)
	expectedSpec := original.Spec
	expectedSpec.Modules.AIHub.ManagementState = aiHubState
	expectedSpec.Modules.Data.ManagementState = dataState

	g.Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		legacy := &configv1alpha1.Platform{}
		if err := cli.Get(ctx, key, legacy); err != nil {
			return err
		}
		legacy.Spec.Modules.ModelRegistry.ManagementState = aiHubState
		legacy.Spec.Modules.FeastOperator.ManagementState = dataState
		if legacy.Annotations == nil {
			legacy.Annotations = make(map[string]string)
		}
		legacy.Annotations[platformConversionE2EAnnotation] = "v1alpha1-write"
		return cli.Update(ctx, legacy)
	})).To(Succeed(), "write Platform through v1alpha1")

	g.Eventually(func(g Gomega) {
		converted := &configv1alpha2.Platform{}
		g.Expect(cli.Get(ctx, key, converted)).To(Succeed())
		g.Expect(converted.Spec).To(Equal(expectedSpec), "v1alpha1 modelregistry/feastoperator should map to v1alpha2 aiHub/data without changing other modules")
		g.Expect(converted.Annotations).To(HaveKeyWithValue(platformConversionE2EAnnotation, "v1alpha1-write"))
	}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

	// Submit a hub-version update as well, then read through the compatibility
	// version to cover the reverse path through the installed conversion webhook.
	g.Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		hub := &configv1alpha2.Platform{}
		if err := cli.Get(ctx, key, hub); err != nil {
			return err
		}
		if hub.Annotations == nil {
			hub.Annotations = make(map[string]string)
		}
		hub.Annotations[platformConversionE2EAnnotation] = "v1alpha2-write"
		return cli.Update(ctx, hub)
	})).To(Succeed(), "write Platform through v1alpha2")

	g.Eventually(func(g Gomega) {
		converted := &configv1alpha1.Platform{}
		g.Expect(cli.Get(ctx, key, converted)).To(Succeed())
		g.Expect(converted.Spec.Modules.ModelRegistry.ManagementState).To(Equal(aiHubState))
		g.Expect(converted.Spec.Modules.FeastOperator.ManagementState).To(Equal(dataState))
		g.Expect(converted.Annotations).To(HaveKeyWithValue(platformConversionE2EAnnotation, "v1alpha2-write"))
	}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
}

func effectivePlatformState(state operatorv1.ManagementState) operatorv1.ManagementState {
	if state == operatorv1.Managed {
		return operatorv1.Managed
	}
	return operatorv1.Removed
}
