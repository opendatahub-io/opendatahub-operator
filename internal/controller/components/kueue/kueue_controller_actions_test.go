//nolint:testpackage
package kueue

import (
	"context"
	"slices"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
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

func TestCheckPreConditions_Unmanaged_KueueOperatorNotInstalled(t *testing.T) {
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

func TestManageKueueAdminRoleBinding_AuthCRNotFound(t *testing.T) {
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

	err = manageKueueAdminRoleBinding(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify no ClusterRoleBinding was created
	g.Expect(rr.Resources).Should(BeEmpty())
}

func TestManageKueueAdminRoleBinding_WithValidAdminGroups(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	authCR := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.AuthInstanceName,
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups: []string{"rhods-admins", "odh-admins", "custom-admin-group"},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(authCR))
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = manageKueueAdminRoleBinding(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify ClusterRoleBinding was created with correct properties
	g.Expect(rr.Resources).Should(HaveLen(1))

	g.Expect(rr.Resources[0]).Should(And(
		jq.Match(`.metadata.name == "%s"`, KueueAdminRoleBindingName),
		jq.Match(`.kind == "ClusterRoleBinding"`),
		jq.Match(`.apiVersion == "rbac.authorization.k8s.io/v1"`),
		jq.Match(`.roleRef.name == "kueue-batch-admin-role"`),
		jq.Match(`.roleRef.kind == "ClusterRole"`),
		jq.Match(`.roleRef.apiGroup == "rbac.authorization.k8s.io"`),
		jq.Match(`.subjects | length == 3`),
		jq.Match(`.subjects[0].kind == "Group"`),
		jq.Match(`.subjects[0].apiGroup == "rbac.authorization.k8s.io"`),
		jq.Match(`.subjects[1].kind == "Group"`),
		jq.Match(`.subjects[1].apiGroup == "rbac.authorization.k8s.io"`),
		jq.Match(`.subjects[2].kind == "Group"`),
		jq.Match(`.subjects[2].apiGroup == "rbac.authorization.k8s.io"`),
		jq.Match(`[.subjects[].name] | sort == ["custom-admin-group", "odh-admins", "rhods-admins"]`),
	))
}

func TestManageKueueAdminRoleBinding_WithFilteredAdminGroups(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	// Simulate upgrade scenario where Auth CR might contain invalid groups
	authCR := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.AuthInstanceName,
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups: []string{"rhods-admins", "system:authenticated", "", "valid-group"},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(authCR))
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = manageKueueAdminRoleBinding(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify ClusterRoleBinding was created with filtered groups (invalid groups excluded)
	g.Expect(rr.Resources).Should(HaveLen(1))

	g.Expect(rr.Resources[0]).Should(And(
		jq.Match(`.metadata.name == "%s"`, KueueAdminRoleBindingName),
		jq.Match(`.kind == "ClusterRoleBinding"`),
		jq.Match(`.apiVersion == "rbac.authorization.k8s.io/v1"`),
		jq.Match(`.roleRef.name == "kueue-batch-admin-role"`),
		jq.Match(`.subjects | length == 2`),
		jq.Match(`[.subjects[].name] | sort == ["rhods-admins", "valid-group"]`),
	))
}

func TestManageKueueAdminRoleBinding_WithOnlyInvalidAdminGroups(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	// Simulate upgrade scenario where Auth CR contains only invalid groups
	authCR := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.AuthInstanceName,
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups: []string{"system:authenticated", ""},
		},
	}

	cli, err := fakeclient.New(fakeclient.WithObjects(authCR))
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = manageKueueAdminRoleBinding(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify ClusterRoleBinding was created but with no subjects (all groups filtered out)
	g.Expect(rr.Resources).Should(HaveLen(1))

	g.Expect(rr.Resources[0]).Should(And(
		jq.Match(`.metadata.name == "%s"`, KueueAdminRoleBindingName),
		jq.Match(`.kind == "ClusterRoleBinding"`),
		jq.Match(`.apiVersion == "rbac.authorization.k8s.io/v1"`),
		jq.Match(`.roleRef.name == "kueue-batch-admin-role"`),
		jq.Match(`.subjects | length == 0`),
	))
}

func TestManageKueueAdminRoleBinding_WithEmptyAdminGroups(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	// Create Auth CR with empty admin groups
	authCR := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.AuthInstanceName,
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups: []string{},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(authCR),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	kueue := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &kueue,
		Conditions: conditions.NewManager(&kueue, status.ConditionTypeReady),
	}

	err = manageKueueAdminRoleBinding(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify that a ClusterRoleBinding was created with no subjects
	resources := rr.Resources
	g.Expect(resources).To(HaveLen(1))

	g.Expect(resources[0]).Should(And(
		jq.Match(`.metadata.name == "%s"`, KueueAdminRoleBindingName),
		jq.Match(`.kind == "ClusterRoleBinding"`),
		jq.Match(`.apiVersion == "rbac.authorization.k8s.io/v1"`),
		jq.Match(`.roleRef.name == "kueue-batch-admin-role"`),
		jq.Match(`.subjects | length == 0`),
	))
}

func TestManageDefaultKueueResourcesAction_NotKueueInstance(t *testing.T) {
	g := NewWithT(t)

	rr := &types.ReconciliationRequest{
		Instance: &componentApi.Dashboard{}, // Wrong type
	}

	err := manageDefaultKueueResourcesAction(context.Background(), rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("is not a componentApi.Kueue"))
}

func TestManageDefaultKueueResourcesAction_RemovedState(t *testing.T) {
	g := NewWithT(t)

	kueue := &componentApi.Kueue{
		Spec: componentApi.KueueSpec{
			KueueManagementSpec: componentApi.KueueManagementSpec{
				ManagementState: operatorv1.Removed,
			},
		},
	}

	rr := &types.ReconciliationRequest{
		Instance: kueue,
	}

	err := manageDefaultKueueResourcesAction(context.Background(), rr)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestDefaultKueueResourcesAction(t *testing.T) {
	defaultClusterQueueName := "defaultClusterQueueName"
	defaultLocalQueueName := "defaultLocalQueueName"
	kueueConfigName := "cluster"

	// Create a managed namespace
	managedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-managed-ns",
			Labels: map[string]string{
				KueueManagedLabelKey: "true",
			},
		},
	}

	// Create a legacy managed namespace
	legacyManagedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-legacy-managed-ns",
			Labels: map[string]string{
				KueueLegacyManagedLabelKey: "true",
			},
		},
	}

	// Create both annotation managed namespace
	bothManagedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-both-managed-ns",
			Labels: map[string]string{
				KueueManagedLabelKey:       "true",
				KueueLegacyManagedLabelKey: "true",
			},
		},
	}

	// And an unmanaged one
	unmanagedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-unmanaged-ns",
		},
	}

	var tests = []struct {
		name                      string
		managedState              operatorv1.ManagementState
		totalResourceCount        int
		expectKueueConfigResource bool
	}{
		{"managed", operatorv1.Managed, 4, false},
		{"unmanaged", operatorv1.Unmanaged, 5, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			kueue := &componentApi.Kueue{
				Spec: componentApi.KueueSpec{
					KueueManagementSpec: componentApi.KueueManagementSpec{
						ManagementState: test.managedState,
					},
					KueueDefaultQueueSpec: componentApi.KueueDefaultQueueSpec{
						DefaultLocalQueueName:   defaultLocalQueueName,
						DefaultClusterQueueName: defaultClusterQueueName,
					},
				},
			}

			client := fake.NewClientBuilder().
				WithRuntimeObjects(managedNamespace, legacyManagedNamespace, bothManagedNamespace, unmanagedNamespace).
				Build()

			rr := &types.ReconciliationRequest{
				Instance: kueue,
				DSCI: &dsciv1.DSCInitialization{
					Spec: dsciv1.DSCInitializationSpec{
						ApplicationsNamespace: xid.New().String(),
					},
				},
				Client:    client,
				Resources: []unstructured.Unstructured{}, // Initialize empty resources
			}

			err := manageDefaultKueueResourcesAction(context.Background(), rr)
			g.Expect(err).ToNot(HaveOccurred())

			// Should have added ClusterQueue and LocalQueue resources
			g.Expect(rr.Resources).To(HaveLen(test.totalResourceCount))

			// Verify ClusterQueue was added
			var clusterQueue *unstructured.Unstructured
			var localQueues []*unstructured.Unstructured
			var kueueConfig *unstructured.Unstructured
			for i := range rr.Resources {
				switch rr.Resources[i].GetKind() {
				case gvk.ClusterQueue.Kind:
					clusterQueue = &rr.Resources[i]
				case gvk.LocalQueue.Kind:
					localQueues = append(localQueues, &rr.Resources[i])
				case gvk.KueueConfigV1.Kind:
					kueueConfig = &rr.Resources[i]
				}
			}

			if test.expectKueueConfigResource {
				g.Expect(kueueConfig).ToNot(BeNil())
				g.Expect(kueueConfig.GetName()).To(Equal(kueueConfigName))
			}

			g.Expect(clusterQueue).ToNot(BeNil())
			g.Expect(clusterQueue.GetName()).To(Equal(defaultClusterQueueName))
			g.Expect(clusterQueue.GetNamespace()).To(BeEmpty()) // ClusterQueue is cluster-scoped

			g.Expect(localQueues).To(HaveLen(3))
			namespacesNames := []string{}
			for _, lc := range localQueues {
				g.Expect(lc).ToNot(BeNil())
				g.Expect(lc.GetName()).To(Equal(defaultLocalQueueName))
				namespacesNames = append(namespacesNames, lc.GetNamespace())
			}
			g.Expect(namespacesNames).To(HaveLen(3))
			g.Expect(slices.Contains(namespacesNames, "test-managed-ns")).Should(BeTrue())
			g.Expect(slices.Contains(namespacesNames, "test-legacy-managed-ns")).Should(BeTrue())
			g.Expect(slices.Contains(namespacesNames, "test-both-managed-ns")).Should(BeTrue())
		})
	}
}
