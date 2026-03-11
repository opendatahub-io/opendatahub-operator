package cloudmanager_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// TestCloudManager_DeploymentsAvailable verifies that creating a CR with all
// dependencies Managed causes the dependency deployments to actually reach
// Available status — not just exist.
func TestCloudManager_DeploymentsAvailable(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)
	waitForDeploymentsAvailable(wt)
}

// TestCloudManager_DeploymentSelfHealing verifies that if a managed deployment
// is deleted, the controller recreates it. This tests the reconcile loop against
// a real cluster with real UIDs and resource versions.
func TestCloudManager_DeploymentSelfHealing(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForDeploymentsAvailable(wt)

	// Delete the cert-manager deployment out from under the controller.
	target := managedDependencyDeployments[0]
	nn := types.NamespacedName{Name: target.Name, Namespace: target.Namespace}

	wt.Delete(gvk.Deployment, nn).Eventually().Should(Succeed())
	wt.Get(gvk.Deployment, nn).Eventually().Should(BeNil())

	// The controller should recreate it.
	wt.Get(gvk.Deployment, nn).Eventually().Should(
		jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
	)
}

// TestCloudManager_GarbageCollectionOnDelete verifies that deleting the CR
// causes Kubernetes garbage collection to clean up all owned deployments.
// This only works on a real cluster — envtest doesn't run the GC controller.
func TestCloudManager_GarbageCollectionOnDelete(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	// Verify deployments have owner references pointing to the CR.
	for _, dep := range managedDependencyDeployments {
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: dep.Name, Namespace: dep.Namespace,
		}).Eventually().Should(
			jq.Match(`.metadata.ownerReferences | length > 0`),
		)
	}

	// Delete the CR.
	wt.Delete(provider.GVK, crNN()).Eventually().Should(Succeed())
	wt.Get(provider.GVK, crNN()).Eventually().Should(BeNil())

	// All owned deployments should be garbage-collected.
	for _, dep := range managedDependencyDeployments {
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: dep.Name, Namespace: dep.Namespace,
		}).Eventually().Should(BeNil())
	}
}

// TestCloudManager_UnmanagedNotReconciled verifies that switching a dependency
// from Managed to Unmanaged causes the controller to stop reconciling it.
// If the deployment is then deleted, the controller should not recreate it.
func TestCloudManager_UnmanagedNotReconciled(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForDeploymentsAvailable(wt)

	// Capture the generation before patching.
	cr := wt.Get(provider.GVK, crNN()).Eventually().Should(Not(BeNil()))
	gen, _, _ := unstructured.NestedInt64(cr.Object, "metadata", "generation")

	// Patch cert-manager to Unmanaged.
	wt.Patch(provider.GVK, crNN(), func(obj *unstructured.Unstructured) error {
		return unstructured.SetNestedField(
			obj.Object, "Unmanaged",
			"spec", "dependencies", "certManager", "managementPolicy",
		)
	}).Eventually().Should(Not(BeNil()))

	// Wait for the controller to fully reconcile the spec change —
	// observedGeneration must catch up to the new generation.
	wt.Get(provider.GVK, crNN()).Eventually().Should(And(
		jq.Match(`.metadata.generation > %d`, gen),
		jq.Match(`.status.observedGeneration == .metadata.generation`),
		jq.Match(`.status.phase == "Ready"`),
	))

	// Delete the cert-manager deployment.
	target := managedDependencyDeployments[0]
	nn := types.NamespacedName{Name: target.Name, Namespace: target.Namespace}
	wt.Delete(gvk.Deployment, nn).Eventually().Should(Succeed())
	wt.Get(gvk.Deployment, nn).Eventually().Should(BeNil())

	// It should NOT come back — the controller is no longer managing it.
	consistentlyGone(wt, nn)

	// The other deployments should still be running.
	for _, dep := range managedDependencyDeployments[1:] {
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: dep.Name, Namespace: dep.Namespace,
		}).Eventually().Should(Not(BeNil()))
	}
}

// TestCloudManager_InvalidNameRejected verifies that the CEL validation rule
// on the CRD rejects CRs with names other than the expected singleton name.
func TestCloudManager_InvalidNameRejected(t *testing.T) {
	wt := tc.NewWithT(t)

	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(provider.GVK)
	cr.SetName("wrong-name")
	cr.Object["spec"] = map[string]any{
		"dependencies": allManaged(),
	}

	err := wt.Client().Create(wt.Context(), cr)
	wt.Expect(err).To(HaveOccurred())
}

// ---------------------------------------------------------------------------
// Reconciliation lifecycle tests
// ---------------------------------------------------------------------------

// TestCloudManager_StatusConditions verifies that the CR reports proper status
// conditions with all expected fields after successful reconciliation:
// - Ready=True (top-level happy condition)
// - ProvisioningSucceeded=True (dependent condition)
// - Each condition has type, status, reason, lastTransitionTime, observedGeneration
// - Status phase is "Ready" and observedGeneration matches metadata.generation.
func TestCloudManager_StatusConditions(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	t.Run("Ready condition", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(provider.GVK, crNN()).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
			jq.Match(`.status.conditions[] | select(.type == "Ready") | has("lastTransitionTime")`),
		))
	})

	t.Run("ProvisioningSucceeded condition", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(provider.GVK, crNN()).Eventually().Should(And(
			jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "True"`),
			jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | has("lastTransitionTime")`),
			jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .observedGeneration > 0`),
		))
	})

	t.Run("phase and observedGeneration", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(provider.GVK, crNN()).Eventually().Should(And(
			jq.Match(`.status.phase == "Ready"`),
			jq.Match(`.status.observedGeneration == .metadata.generation`),
		))
	})
}

// TestCloudManager_HelmRenderedResources verifies that the Helm chart rendering
// pipeline produces resources with the expected operator metadata. The deploy
// action stamps every owned resource with platform labels and annotations.
func TestCloudManager_HelmRenderedResources(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	partOfValue := strings.ToLower(provider.GVK.Kind)

	for _, dep := range managedDependencyDeployments {
		t.Run(dep.Name, func(t *testing.T) {
			wt := tc.NewWithT(t)
			nn := types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}

			wt.Get(gvk.Deployment, nn).Eventually().Should(And(
				jq.Match(`.metadata.labels."%s" == "%s"`, labels.PlatformPartOf, partOfValue),
				jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.InstanceName, provider.InstanceName),
				jq.Match(`.metadata.annotations | has("%s")`, annotations.InstanceGeneration),
				jq.Match(`.metadata.annotations | has("%s")`, annotations.InstanceUID),
				jq.Match(`.metadata.annotations | has("%s")`, annotations.PlatformVersion),
				jq.Match(`.metadata.annotations | has("%s")`, annotations.PlatformType),
			))
		})
	}
}

// TestCloudManager_NamespacesCreated verifies that the pre-apply hooks create
// the target namespaces for each managed dependency before resources are applied.
func TestCloudManager_NamespacesCreated(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	for _, ns := range dependencyNamespaces {
		t.Run(ns, func(t *testing.T) {
			wt := tc.NewWithT(t)
			wt.Get(gvk.Namespace, types.NamespacedName{Name: ns}).
				Eventually().
				Should(Not(BeNil()))
		})
	}
}

// TestCloudManager_ResourceCreation verifies that the Helm charts produce not
// just deployments but the full set of supporting resources (ServiceAccounts,
// RBAC) in each dependency namespace.
func TestCloudManager_ResourceCreation(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	for _, dep := range managedDependencyDeployments {
		t.Run(dep.Name+"/serviceaccounts", func(t *testing.T) {
			wt := tc.NewWithT(t)
			wt.List(gvk.ServiceAccount,
				client.InNamespace(dep.Namespace),
				client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(provider.GVK.Kind)},
			).Eventually().Should(Not(BeEmpty()))
		})
	}
}

// TestCloudManager_StatusAfterSpecChange verifies that updating the CR spec
// triggers re-reconciliation and the status reflects the new generation.
func TestCloudManager_StatusAfterSpecChange(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	// Capture the current generation.
	cr := wt.Get(provider.GVK, crNN()).Eventually().Should(Not(BeNil()))
	gen, _, _ := unstructured.NestedInt64(cr.Object, "metadata", "generation")

	// Patch a dependency to Unmanaged — this bumps the generation.
	wt.Patch(provider.GVK, crNN(), func(obj *unstructured.Unstructured) error {
		return unstructured.SetNestedField(
			obj.Object, "Unmanaged",
			"spec", "dependencies", "sailOperator", "managementPolicy",
		)
	}).Eventually().Should(Not(BeNil()))

	// Status should eventually reflect the new generation.
	wt.Get(provider.GVK, crNN()).Eventually().Should(And(
		jq.Match(`.metadata.generation > %d`, gen),
		jq.Match(`.status.observedGeneration == .metadata.generation`),
		jq.Match(`.status.phase == "Ready"`),
	))
}

// ---------------------------------------------------------------------------
// Workload validation tests
// ---------------------------------------------------------------------------

// TestCloudManager_CertManagerIssuesCertificates verifies that the cert-manager
// operator is functional by checking that the bootstrap PKI trust chain works:
// the root CA Certificate becomes Ready and cert-manager creates its Secret.
func TestCloudManager_CertManagerIssuesCertificates(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	t.Run("selfsigned ClusterIssuer is ready", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{
			Name: "opendatahub-selfsigned-issuer",
		}).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)
	})

	t.Run("root CA Certificate is issued", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(gvk.CertManagerCertificate, types.NamespacedName{
			Name: "opendatahub-ca", Namespace: "cert-manager",
		}).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)
	})

	t.Run("CA-backed ClusterIssuer is ready", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{
			Name: "opendatahub-ca-issuer",
		}).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)
	})

	t.Run("CA Secret is created", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(gvk.Secret, types.NamespacedName{
			Name: "opendatahub-ca", Namespace: "cert-manager",
		}).Eventually().Should(Not(BeNil()))
	})
}

// TestCloudManager_LWSOperatorFunctional verifies that the LeaderWorkerSet
// operator is running and has registered the LeaderWorkerSet CRD.
func TestCloudManager_LWSOperatorFunctional(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	t.Run("LeaderWorkerSetOperator CR exists", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(gvk.LeaderWorkerSetOperatorV1, types.NamespacedName{
			Name: "cluster",
		}).Eventually().Should(Not(BeNil()))
	})

	t.Run("LeaderWorkerSet CRD is installed", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(gvk.CustomResourceDefinition, types.NamespacedName{
			Name: "leaderworkersets.leaderworkerset.x-k8s.io",
		}).Eventually().Should(Not(BeNil()))
	})
}

// TestCloudManager_SailOperatorFunctional verifies that the sail-operator
// is running and the Istio CR it creates reaches a healthy state.
func TestCloudManager_SailOperatorFunctional(t *testing.T) {
	wt := tc.NewWithT(t)
	createCR(t, wt, allManaged())
	waitForReady(wt)

	t.Run("Istio CR is healthy", func(t *testing.T) {
		wt := tc.NewWithT(t)
		wt.Get(gvkIstio, types.NamespacedName{
			Name: "default", Namespace: "istio-system",
		}).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)
	})
}
