//nolint:testpackage
package dashboard

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

	fakeSchema.AddKnownTypeWithName(gvk.DashboardHardwareProfile, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(dashboardHardwareProfileListGVK, &unstructured.UnstructuredList{})
	fakeSchema.AddKnownTypeWithName(gvk.HardwareProfile, &infraAPI.HardwareProfile{})
	fakeSchema.AddKnownTypeWithName(
		schema.GroupVersionKind{
			Group:   "apiextensions.k8s.io",
			Version: "v1",
			Kind:    "CustomResourceDefinition",
		},
		&apiextensionsv1.CustomResourceDefinition{},
	)

	// Add dashboard HWProfile CRD to the fake client
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hardwareprofiles.dashboard.opendatahub.io",
		},
	}

	mockDashboardHardwareProfile := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "dashboard.opendatahub.io/v1alpha1",
			"kind":       "HardwareProfile",
			"metadata": map[string]any{
				"name":      "test-name",
				"namespace": "test-namespace",
			},
			"spec": map[string]any{
				"displayName":  "Test Display Name",
				"enabled":      true,
				"description":  "Test Description",
				"tolerations":  []any{},
				"nodeSelector": map[string]any{},
				"identifiers":  []any{},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(crd, mockDashboardHardwareProfile),
		fakeclient.WithScheme(fakeSchema),
	)
	g.Expect(err).ShouldNot(HaveOccurred())
	rr := &types.ReconciliationRequest{
		Client: cli,
	}

	err = reconcileHardwareProfiles(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	var createdInfraHWProfile infraAPI.HardwareProfile
	err = cli.Get(ctx, client.ObjectKey{
		Name:      "test-name",
		Namespace: "test-namespace",
	}, &createdInfraHWProfile)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(createdInfraHWProfile.Name).Should(Equal("test-name"))
	g.Expect(createdInfraHWProfile.Namespace).Should(Equal("test-namespace"))
	g.Expect(createdInfraHWProfile.Spec.SchedulingSpec.SchedulingType).Should(Equal(infraAPI.NodeScheduling))
	g.Expect(createdInfraHWProfile.GetAnnotations()["opendatahub.io/display-name"]).Should(Equal("Test Display Name"))
	g.Expect(createdInfraHWProfile.GetAnnotations()["opendatahub.io/description"]).Should(Equal("Test Description"))
	g.Expect(createdInfraHWProfile.GetAnnotations()["opendatahub.io/disabled"]).Should(Equal("false"))
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

	logger := log.FromContext(ctx)

	mockDashboardHardwareProfile := &DashboardHardwareProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-namespace",
		},
		Spec: DashboardHardwareProfileSpec{
			DisplayName:  "Test Display Name",
			Enabled:      true,
			Description:  "Test Description",
			Tolerations:  nil,
			NodeSelector: nil,
			Identifiers:  nil,
		},
	}

	var receivedHardwareProfile infraAPI.HardwareProfile

	err = createInfraHWP(ctx, rr, logger, mockDashboardHardwareProfile)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cli.Get(ctx, client.ObjectKey{
		Name:      "test-name",
		Namespace: "test-namespace",
	}, &receivedHardwareProfile)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(receivedHardwareProfile.Name).Should(Equal("test-name"))
	g.Expect(receivedHardwareProfile.Namespace).Should(Equal("test-namespace"))
	g.Expect(receivedHardwareProfile.GetAnnotations()["opendatahub.io/display-name"]).Should(Equal("Test Display Name"))
	g.Expect(receivedHardwareProfile.GetAnnotations()["opendatahub.io/description"]).Should(Equal("Test Description"))
	g.Expect(receivedHardwareProfile.GetAnnotations()["opendatahub.io/disabled"]).Should(Equal("false"))
}
