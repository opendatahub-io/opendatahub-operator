//nolint:testpackage
package kueue

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions_Unknown_State(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&kueue).Should(
		WithTransform(resources.ToUnstructured,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionUnknown),
		),
	)
}

func TestCheckPreConditions_CRD_MultiKueueConfigV1Alpha1(t *testing.T) { //nolint:dupl
	ctx := context.Background()
	g := NewWithT(t)

	fakeMultiKueueConfigV1Alpha1 := mocks.NewMockCRD("kueue.x-k8s.io", "v1alpha1", "MultiKueueConfig", "fakeName")
	fakeMultiKueueConfigV1Alpha1.Status.StoredVersions = append(fakeMultiKueueConfigV1Alpha1.Status.StoredVersions, "v1alpha1")
	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	fakeSchema.AddKnownTypeWithName(gvk.MultiKueueConfigV1Alpha1, &unstructured.Unstructured{})
	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			fakeMultiKueueConfigV1Alpha1,
		),
		fakeclient.WithScheme(
			fakeSchema,
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(status.MultiKueueCRDMessage)))
}

func TestCheckPreConditions_CRD_MultikueueClusterV1Alpha1(t *testing.T) { //nolint:dupl
	ctx := context.Background()
	g := NewWithT(t)

	fakeMultikueueClusterV1Alpha1 := mocks.NewMockCRD("kueue.x-k8s.io", "v1alpha1", "MultiKueueCluster", "fakeName")
	fakeMultikueueClusterV1Alpha1.Status.StoredVersions = append(fakeMultikueueClusterV1Alpha1.Status.StoredVersions, "v1alpha1")
	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	fakeSchema.AddKnownTypeWithName(gvk.MultikueueClusterV1Alpha1, &unstructured.Unstructured{})
	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			fakeMultikueueClusterV1Alpha1,
		),
		fakeclient.WithScheme(
			fakeSchema,
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(status.MultiKueueCRDMessage)))
}

func TestCheckPreConditions_CRD_MultiKueueConfigV1Alpha1_and_MultikueueClusterV1Alpha1(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	fakeMultiKueueConfigV1Alpha1 := mocks.NewMockCRD("kueue.x-k8s.io", "v1alpha1", "MultiKueueConfig", "fakeName")
	fakeMultiKueueConfigV1Alpha1.Status.StoredVersions = append(fakeMultiKueueConfigV1Alpha1.Status.StoredVersions, "v1alpha1")
	fakeMultikueueClusterV1Alpha1 := mocks.NewMockCRD("kueue.x-k8s.io", "v1alpha1", "MultiKueueCluster", "fakeName")
	fakeMultikueueClusterV1Alpha1.Status.StoredVersions = append(fakeMultikueueClusterV1Alpha1.Status.StoredVersions, "v1alpha1")
	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	fakeSchema.AddKnownTypeWithName(gvk.MultiKueueConfigV1Alpha1, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(gvk.MultikueueClusterV1Alpha1, &unstructured.Unstructured{})
	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			fakeMultiKueueConfigV1Alpha1,
			fakeMultikueueClusterV1Alpha1,
		),
		fakeclient.WithScheme(
			fakeSchema,
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(status.MultiKueueCRDMessage)))
}

func TestCheckPreConditions_Managed_KueueOperatorAlreadyInstalled(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: kueueOperator,
			}},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{
		Spec: componentApi.KueueSpec{
			KueueManagementSpec: componentApi.KueueManagementSpec{
				ManagementState: operatorv1.Managed,
			},
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(status.KueueOperatorAlreadyInstalledMessage)))
}

func TestCheckPreConditions_Managed_KueueOperatorNotInstalled(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{
		Spec: componentApi.KueueSpec{
			KueueManagementSpec: componentApi.KueueManagementSpec{
				ManagementState: operatorv1.Unmanaged,
			},
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(status.KueueOperatorNotInstalledMessage)))
}

func TestConfigureClusterQueueViewerRoleAction_RoleNotFound(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = configureClusterQueueViewerRoleAction(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestConfigureClusterQueueViewerRoleAction(t *testing.T) {
	roleWithTrueLabel := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: map[string]string{KueueBatchUserLabel: "true"},
		},
	}
	roleWithFalseLabel := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: map[string]string{KueueBatchUserLabel: "false"},
		},
	}
	roleWithMissingLabel := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: map[string]string{},
		},
	}
	roleWithNilLabels := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: nil,
		},
	}
	var tests = []struct {
		name        string
		clusterRole *rbacv1.ClusterRole
	}{
		{"labelIsTrue", roleWithTrueLabel},
		{"labelIsFalse", roleWithFalseLabel},
		{"labelIsMissing", roleWithMissingLabel},
		{"labelsNil", roleWithNilLabels},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			g := NewWithT(t)

			cli, err := fakeclient.New(fakeclient.WithObjects(test.clusterRole))
			g.Expect(err).ShouldNot(HaveOccurred())

			ks := componentApi.Kueue{}

			rr := types.ReconciliationRequest{
				Client:     cli,
				Instance:   &ks,
				Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
			}

			err = configureClusterQueueViewerRoleAction(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())
			err = cli.Get(ctx, client.ObjectKeyFromObject(test.clusterRole), test.clusterRole)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(test.clusterRole.Labels[KueueBatchUserLabel]).Should(Equal("true"))
		})
	}
}

func TestInitializeAction_Managed(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{
		Spec: componentApi.KueueSpec{
			KueueManagementSpec: componentApi.KueueManagementSpec{
				ManagementState: operatorv1.Managed,
			},
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = initialize(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Manifests).Should(ConsistOf(manifestsPath(), kueueConfigManifestsPath()))
}

func TestInitializeAction_Unmanaged(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{
		Spec: componentApi.KueueSpec{
			KueueManagementSpec: componentApi.KueueManagementSpec{
				ManagementState: operatorv1.Unmanaged,
			},
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = initialize(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Manifests).Should(ConsistOf(kueueConfigManifestsPath()))
}
