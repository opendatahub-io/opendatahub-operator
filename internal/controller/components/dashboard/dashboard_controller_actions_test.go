//nolint:testpackage
package dashboard

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infraAPI "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestMigrateHardwareProfiles(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dashboardHardwareProfileListGVK := schema.GroupVersionKind{
		Group:   "dashboard.opendatahub.io",
		Version: "v1alpha1",
		Kind:    "HardwareProfileList",
	}

	fakeSchema.AddKnownTypeWithName(gvk.DashboardHardwareProfile, &infraAPI.DashboardHardwareProfile{})
	fakeSchema.AddKnownTypeWithName(dashboardHardwareProfileListGVK, &infraAPI.DashboardHardwareProfileList{})
	fakeSchema.AddKnownTypeWithName(gvk.HardwareProfile, &infraAPI.HardwareProfile{})

	mockDashboardHardwareProfile := &infraAPI.DashboardHardwareProfile{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test name",
			Namespace: "test namespace",
		},
		Spec: infraAPI.DashboardHardwareProfileSpec{
			Tolerations:  []corev1.Toleration{},
			NodeSelector: make(map[string]string),
			Identifiers:  []infraAPI.HardwareIdentifier{},
		},
	}
	cli, err := fakeclient.New(
		fakeclient.WithObjects(mockDashboardHardwareProfile),
		fakeclient.WithScheme(fakeSchema),
	)
	g.Expect(err).ShouldNot(HaveOccurred())
	rr := &types.ReconciliationRequest{
		Client: cli,
	}

	err = migrateHardwareProfiles(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	var updateddashboardHWProfile infraAPI.DashboardHardwareProfile
	err = cli.Get(ctx, client.ObjectKeyFromObject(mockDashboardHardwareProfile), &updateddashboardHWProfile)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updateddashboardHWProfile.GetAnnotations()).Should(HaveKey("migrated-to"))

	var updatedHWProfile infraAPI.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{
		Name:      mockDashboardHardwareProfile.Name,
		Namespace: mockDashboardHardwareProfile.Namespace,
	}, &updatedHWProfile)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(updatedHWProfile.GetAnnotations()).Should(HaveKey("migrated-from"))
}

func TestCreateInfraHardwareProfile(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	fakeSchema.AddKnownTypeWithName(gvk.HardwareProfile, &infraAPI.HardwareProfile{})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(),
		fakeclient.WithScheme(fakeSchema),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client: cli,
	}

	mockDashboardHardwareProfile := &infraAPI.DashboardHardwareProfile{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test name",
			Namespace: "test namespace",
		},
		Spec: infraAPI.DashboardHardwareProfileSpec{
			Tolerations:  []corev1.Toleration{},
			NodeSelector: make(map[string]string),
			Identifiers:  []infraAPI.HardwareIdentifier{},
		},
	}

	var receivedHardwareProfile infraAPI.HardwareProfile

	err = createInfraHardwareProfile(ctx, rr, mockDashboardHardwareProfile)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Get(ctx, client.ObjectKeyFromObject(mockDashboardHardwareProfile), &receivedHardwareProfile)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(receivedHardwareProfile.Name).Should(Equal("test name"))
	g.Expect(receivedHardwareProfile.Namespace).Should(Equal("test namespace"))
}
