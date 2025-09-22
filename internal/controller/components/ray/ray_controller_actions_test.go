//nolint:testpackage
package ray

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

func TestPerformV3UpgradeSanityChecks(t *testing.T) {
	codeflareComponentName := "default-codeflare"

	t.Run("returns no error if CodeFlare CRD is not present", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithRay(operatorv1.Managed)
		codeFlare := createCodeFlareCR(codeflareComponentName)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, codeFlare))
		g.Expect(err).ShouldNot(HaveOccurred())

		ray := componentApi.Ray{}

		rr := &types.ReconciliationRequest{
			Client:     cli,
			Instance:   &ray,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		}

		err = performV3UpgradeSanityChecks(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("returns no error if CodeFlare CRD exists but no CodeFlare CR is present", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithRay(operatorv1.Managed)

		mockCodeFlareCRD := mocks.NewMockCRD("components.platform.opendatahub.io", "v1alpha1", "CodeFlare", "fakeName")
		mockCodeFlareCRD.Status.StoredVersions = append(mockCodeFlareCRD.Status.StoredVersions, "v1alpha1")

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsc, mockCodeFlareCRD),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		ray := componentApi.Ray{}

		rr := &types.ReconciliationRequest{
			Client:     cli,
			Instance:   &ray,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		}

		err = performV3UpgradeSanityChecks(ctx, rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("returns error if CodeFlare CR is present", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithRay(operatorv1.Managed)
		codeFlare := createCodeFlareCR(codeflareComponentName)

		mockCodeFlareCRD := mocks.NewMockCRD("components.platform.opendatahub.io", "v1alpha1", "CodeFlare", "fakeName")
		mockCodeFlareCRD.Status.StoredVersions = append(mockCodeFlareCRD.Status.StoredVersions, "v1alpha1")

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsc, codeFlare, mockCodeFlareCRD),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		ray := componentApi.Ray{}

		rr := &types.ReconciliationRequest{
			Client:     cli,
			Instance:   &ray,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		}

		err = performV3UpgradeSanityChecks(ctx, rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err).To(MatchError(ContainSubstring(status.CodeFlarePresentMessage)))
	})
}

func createCodeFlareCR(name string) *unstructured.Unstructured {
	c := &unstructured.Unstructured{}
	c.SetGroupVersionKind(gvk.CodeFlare)
	c.SetName(name)

	return c
}
