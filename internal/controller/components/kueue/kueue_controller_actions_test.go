//nolint:testpackage
package kueue

import (
	"slices"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions_Unknown_State(t *testing.T) {
	ctx := t.Context()
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

func TestCheckPreConditions_Managed_KueueOperatorAlreadyInstalled(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      kueueOperator,
					Namespace: kueueOperatorNamespace,
				},
				Spec: &ofapiv1alpha1.SubscriptionSpec{
					Package: kueueOperator,
				},
			},
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
	g.Expect(err).To(MatchError(ContainSubstring(status.KueueStateManagedNotSupportedMessage)))
}

func TestCheckPreConditions_Unmanaged_KueueOperatorNotInstalled(t *testing.T) {
	ctx := t.Context()
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
	ctx := t.Context()
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
			ctx := t.Context()
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
	ctx := t.Context()
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
	g.Expect(rr.Manifests).Should(BeEmpty())
}

func TestInitializeAction_Unmanaged(t *testing.T) {
	ctx := t.Context()
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
	g.Expect(rr.Manifests).Should(BeEmpty())
}

func TestManageKueueAdminRoleBinding_AuthCRNotFound(t *testing.T) {
	ctx := t.Context()
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
	ctx := t.Context()
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
	ctx := t.Context()
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
	ctx := t.Context()
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
	ctx := t.Context()
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

	err := manageDefaultKueueResourcesAction(t.Context(), rr)
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

	err := manageDefaultKueueResourcesAction(t.Context(), rr)
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
				cluster.KueueManagedLabelKey: "true",
			},
		},
	}

	// Create a legacy managed namespace
	legacyManagedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-legacy-managed-ns",
			Labels: map[string]string{
				cluster.KueueLegacyManagedLabelKey: "true",
			},
		},
	}

	// Create both annotation managed namespace
	bothManagedNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-both-managed-ns",
			Labels: map[string]string{
				cluster.KueueManagedLabelKey:       "true",
				cluster.KueueLegacyManagedLabelKey: "true",
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
		withGPU                   bool
	}{
		{"managed", operatorv1.Managed, 5, false, false},
		{"unmanaged", operatorv1.Unmanaged, 6, true, false},
		{"managedWithGPU", operatorv1.Managed, 7, false, true},
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

			// Create DSCI for ApplicationNamespace lookup
			dsci := &dsciv2.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsci",
				},
				Spec: dsciv2.DSCInitializationSpec{
					ApplicationsNamespace: xid.New().String(),
				},
			}

			runtimeObjects := []client.Object{
				managedNamespace,
				legacyManagedNamespace,
				bothManagedNamespace,
				unmanagedNamespace,
				dsci,
			}

			clusterNodes := getClusterNodes(t, test.withGPU)
			runtimeObjects = append(runtimeObjects, clusterNodes...)

			client, err := fakeclient.New(
				fakeclient.WithObjects(runtimeObjects...),
			)
			g.Expect(err).ToNot(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Instance:  kueue,
				Client:    client,
				Resources: []unstructured.Unstructured{}, // Initialize empty resources
			}

			err = manageDefaultKueueResourcesAction(t.Context(), rr)
			g.Expect(err).ToNot(HaveOccurred())

			// Should have added ClusterQueue and LocalQueue resources
			g.Expect(rr.Resources).To(HaveLen(test.totalResourceCount))

			// Verify ClusterQueue was added
			var clusterQueue *unstructured.Unstructured
			var localQueues []*unstructured.Unstructured
			var kueueConfig *unstructured.Unstructured
			var resourceFlavors []*unstructured.Unstructured
			for i := range rr.Resources {
				switch rr.Resources[i].GetKind() {
				case gvk.ClusterQueue.Kind:
					clusterQueue = &rr.Resources[i]
				case gvk.LocalQueue.Kind:
					localQueues = append(localQueues, &rr.Resources[i])
				case gvk.KueueConfigV1.Kind:
					kueueConfig = &rr.Resources[i]
				case gvk.ResourceFlavor.Kind:
					resourceFlavors = append(resourceFlavors, &rr.Resources[i])
				}
			}

			if test.expectKueueConfigResource {
				g.Expect(kueueConfig).ToNot(BeNil())
				g.Expect(kueueConfig.GetName()).To(Equal(kueueConfigName))
			}

			flavorNames := []string{DefaultFlavorName}
			if test.withGPU {
				flavorNames = append(flavorNames, NvidiaFlavorName, AMDFlavorName)
			}
			g.Expect(resourceFlavors).To(HaveLen(len(flavorNames)))
			for _, rf := range resourceFlavors {
				g.Expect(rf.GetName()).To(BeElementOf(flavorNames))
				g.Expect(rf.GetNamespace()).To(BeEmpty()) // ResourceFlavor is cluster-scoped
				g.Expect(rf.GetAnnotations()).To(Equal(map[string]string{
					annotations.ManagedByODHOperator: "false",
				}))
			}

			assertClusterQueueCorrectness(g, clusterQueue, test.withGPU, defaultClusterQueueName, flavorNames)

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

func assertClusterQueueCorrectness(g *WithT, clusterQueue *unstructured.Unstructured, withGPU bool, expectedClusterQueueName string, expectedFlavorNames []string) {
	g.Expect(clusterQueue).ToNot(BeNil())
	g.Expect(clusterQueue.GetName()).To(Equal(expectedClusterQueueName))
	g.Expect(clusterQueue.GetAnnotations()).To(Equal(map[string]string{
		annotations.ManagedByODHOperator: "false",
	}))
	g.Expect(clusterQueue.GetNamespace()).To(BeEmpty()) // ClusterQueue is cluster-scoped
	namespaceSelector, ok, err := unstructured.NestedMap(clusterQueue.Object, "spec", "namespaceSelector")
	g.Expect(ok).To(BeTrue())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(namespaceSelector).To(Equal(map[string]any{
		"matchLabels": map[string]any{
			cluster.KueueManagedLabelKey: "true",
		},
	}))
	resourceGroups, ok, err := unstructured.NestedSlice(clusterQueue.Object, "spec", "resourceGroups")
	g.Expect(ok).To(BeTrue())
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(resourceGroups).To(HaveLen(len(expectedFlavorNames)))
	defaultResourceGroup := map[string]any{
		"coveredResources": []any{
			"cpu",
			"memory",
		},
		"flavors": []any{
			map[string]any{
				"name": DefaultFlavorName,
				"resources": []any{
					map[string]any{
						"name":         "cpu",
						"nominalQuota": "2500m",
					},
					map[string]any{
						"name":         "memory",
						"nominalQuota": "2500Mi",
					},
				},
			},
		},
	}
	g.Expect(resourceGroups[0]).To(Equal(defaultResourceGroup))

	if withGPU {
		amdResourceGroup := map[string]any{
			"coveredResources": []any{AMDGPUResourceKey},
			"flavors": []any{
				map[string]any{
					"name": AMDFlavorName,
					"resources": []any{
						map[string]any{
							"name":         AMDGPUResourceKey,
							"nominalQuota": "7",
						},
					},
				},
			},
		}
		g.Expect(resourceGroups[1]).To(Equal(amdResourceGroup))

		nvidiaResourceGroup := map[string]any{
			"coveredResources": []any{NvidiaGPUResourceKey},
			"flavors": []any{
				map[string]any{
					"name": NvidiaFlavorName,
					"resources": []any{
						map[string]any{
							"name":         NvidiaGPUResourceKey,
							"nominalQuota": "4",
						},
					},
				},
			},
		}
		g.Expect(resourceGroups[2]).To(Equal(nvidiaResourceGroup))
	}
}

func getClusterNodes(t *testing.T, withGPU bool) []client.Object {
	t.Helper()

	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-01",
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1000Mi"),
			},
		},
	}

	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-02",
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1500m"),
				corev1.ResourceMemory: resource.MustParse("1500Mi"),
			},
		},
	}

	node1WithGPU := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-03",
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1000Mi"),
				NvidiaGPUResourceKey:  resource.MustParse("1"),
				AMDGPUResourceKey:     resource.MustParse("2"),
			},
		},
	}

	node2WithGPU := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-04",
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1500m"),
				corev1.ResourceMemory: resource.MustParse("1500Mi"),
				NvidiaGPUResourceKey:  resource.MustParse("3"),
				AMDGPUResourceKey:     resource.MustParse("5"),
			},
		},
	}

	if withGPU {
		return []client.Object{
			node1WithGPU,
			node2WithGPU,
		}
	}
	return []client.Object{
		node1,
		node2,
	}
}
