package cloudmanager_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmapi "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// TestCloudManager_InvalidNameRejected verifies that the CEL validation rule
// on the CRD rejects CRs with names other than the expected singleton name.
// This test does not need a CR to be created first.
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

// TestCloudManager runs all cloud manager e2e tests sequentially under a single
// CR lifecycle. Tests are ordered so that the CR is created once and reused as
// long as possible, minimizing repeated create/teardown cycles:
//
//  1. DeploymentsAvailable — verify initial deploy
//  2. ReadOnlyValidation  — status, labels, workload checks, self-healing
//  3. StatusAfterSpecChange — mutates spec but restores to all-Managed
//  4. UnmanagedNotReconciled — switches cert-manager to Unmanaged
//  5. GarbageCollection — GC action: stale deletion, protected PKI, unmanaged transition
//  6. CascadeDeletionOnCRDelete — Kubernetes cascade via ownerReferences (must be last)
func TestCloudManager(t *testing.T) { //nolint:maintidx // sequential subtests sharing one CR lifecycle are clearer inline
	wt := tc.NewWithT(t)

	cr := newCloudManagerCR(allManaged())
	wt.Create(cr, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))

	// Safety net: if any test fails before GarbageCollectionOnDelete runs,
	// clean up the CR so the next local run starts fresh.
	t.Cleanup(func() {
		_ = wt.Client().Delete(wt.Context(), newCloudManagerCR(allManaged()))
	})

	waitForReady(wt)

	// --- 1. DeploymentsAvailable ---
	// Verifies that dependency namespaces and deployments are created,
	// with deployments reaching Available status.
	t.Run("DeploymentsAvailable", func(t *testing.T) {
		wt := tc.NewWithT(t)

		for _, ns := range common.ManagedNamespaces() {
			t.Run("namespace/"+ns, func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(gvk.Namespace, types.NamespacedName{Name: ns}).
					Eventually().
					Should(Not(BeNil()))
			})
		}

		waitForDeploymentsAvailable(wt)
	})

	// --- 2. ReadOnlyValidation ---
	// Groups tests that do not mutate the engine CR: status checks, label/annotation
	// verification, workload validation, and deployment self-healing.
	t.Run("ReadOnlyValidation", func(t *testing.T) {
		t.Run("StatusConditions", func(t *testing.T) {
			t.Run("Ready condition", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
					jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
					jq.Match(`.status.conditions[] | select(.type == "Ready") | has("lastTransitionTime")`),
				))
			})

			t.Run("ProvisioningSucceeded condition", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
					jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .status == "True"`),
					jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | has("lastTransitionTime")`),
					jq.Match(`.status.conditions[] | select(.type == "ProvisioningSucceeded") | .observedGeneration > 0`),
				))
			})

			t.Run("phase and observedGeneration", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
					jq.Match(`.status.phase == "Ready"`),
					jq.Match(`.status.observedGeneration == .metadata.generation`),
				))
			})
		})

		t.Run("HelmRenderedResources", func(t *testing.T) {
			partOfValue := strings.ToLower(provider.GVK.Kind)

			for _, dep := range managedDependencyDeployments {
				t.Run(dep.Name, func(t *testing.T) {
					wt := tc.NewWithT(t)
					nn := types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}

					wt.Get(gvk.Deployment, nn).Eventually().Should(And(
						jq.Match(`.metadata.labels."%s" == "%s"`, labels.InfrastructurePartOf, partOfValue),
						Not(jq.Match(`.metadata.labels | has("%s")`, labels.PlatformPartOf)),
						jq.Match(`.metadata.annotations."%s" == "%s"`,
							labels.ODHInfrastructurePrefix+annotations.SuffixInstanceName, provider.InstanceName),
						Not(jq.Match(`.metadata.annotations | has("%s")`, annotations.InstanceName)),
					))
				})
			}
		})

		t.Run("ServiceAccountsCreated", func(t *testing.T) {
			for _, dep := range managedDependencyDeployments {
				t.Run(dep.Name, func(t *testing.T) {
					wt := tc.NewWithT(t)
					wt.List(gvk.ServiceAccount,
						client.InNamespace(dep.Namespace),
						client.MatchingLabels{labels.InfrastructurePartOf: strings.ToLower(provider.GVK.Kind)},
					).Eventually().Should(Not(BeEmpty()))
				})
			}
		})

		t.Run("CertManagerIssuesCertificates", func(t *testing.T) {
			t.Run("selfsigned ClusterIssuer is ready", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{
					Name: pki.IssuerName,
				}).Eventually().Should(
					jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
				)
			})

			t.Run("root CA Certificate is issued", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(gvk.CertManagerCertificate, types.NamespacedName{
					Name: pki.CertName, Namespace: pki.CertManagerNamespace,
				}).Eventually().Should(
					jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
				)
			})

			t.Run("CA-backed ClusterIssuer is ready", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{
					Name: pki.CAIssuerName,
				}).Eventually().Should(
					jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
				)
			})

			t.Run("CA Secret is created", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(gvk.Secret, types.NamespacedName{
					Name: pki.CertName, Namespace: pki.CertManagerNamespace,
				}).Eventually().Should(Not(BeNil()))
			})
		})

		t.Run("LWSOperatorFunctional", func(t *testing.T) {
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
		})

		t.Run("GatewayAPICRDsInstalled", func(t *testing.T) {
			gatewayAPICRDs := []string{
				"backendtlspolicies.gateway.networking.k8s.io",
				"gatewayclasses.gateway.networking.k8s.io",
				"gateways.gateway.networking.k8s.io",
				"grpcroutes.gateway.networking.k8s.io",
				"httproutes.gateway.networking.k8s.io",
				"referencegrants.gateway.networking.k8s.io",
			}

			for _, crdName := range gatewayAPICRDs {
				t.Run(crdName, func(t *testing.T) {
					wt := tc.NewWithT(t)
					wt.Get(gvk.CustomResourceDefinition, types.NamespacedName{
						Name: crdName,
					}).Eventually().Should(Not(BeNil()))
				})
			}
		})

		t.Run("SailOperatorFunctional", func(t *testing.T) {
			t.Run("Istio CR is healthy", func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(gvk.Istio, types.NamespacedName{
					Name: "default", Namespace: "istio-system",
				}).Eventually().Should(
					jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
				)
			})
		})

		// DeploymentSelfHealing: deletes each managed deployment and verifies the
		// controller recreates it with a new UID. The engine CR itself is not mutated.
		t.Run("DeploymentSelfHealing", func(t *testing.T) {
			for _, dep := range managedDependencyDeployments {
				t.Run(dep.Name, func(t *testing.T) {
					wt := tc.NewWithT(t)
					nn := types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}

					// Capture the original UID before deleting.
					original := wt.Get(gvk.Deployment, nn).Eventually().Should(Not(BeNil()))
					originalUID := string(original.GetUID())

					wt.Delete(gvk.Deployment, nn).Eventually().Should(Succeed())

					// The controller should recreate it with a new UID.
					wt.Get(gvk.Deployment, nn).Eventually().Should(And(
						jq.Match(`.metadata.uid != "%s"`, originalUID),
						jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`),
					))
				})
			}
		})
	})

	// --- 3. StatusAfterSpecChange ---
	// Verifies that updating the CR spec triggers re-reconciliation and the
	// status reflects the new generation. Mutates the spec (Managed→Unmanaged→Managed)
	// but restores it, so subsequent tests can still use the same CR.
	t.Run("StatusAfterSpecChange", func(t *testing.T) {
		wt := tc.NewWithT(t)

		cr := wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))
		gen1, _, _ := unstructured.NestedInt64(cr.Object, "metadata", "generation")

		// First mutation: switch sailOperator to Unmanaged.
		wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(
				obj.Object, string(ccmapi.Unmanaged),
				"spec", "dependencies", "sailOperator", "managementPolicy",
			)
		}).Eventually().Should(Not(BeNil()))

		wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
			jq.Match(`.metadata.generation > %d`, gen1),
			jq.Match(`.status.observedGeneration == .metadata.generation`),
			jq.Match(`.status.phase == "Ready"`),
		))

		// Second mutation: switch it back to Managed.
		cr = wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))
		gen2, _, _ := unstructured.NestedInt64(cr.Object, "metadata", "generation")

		wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(
				obj.Object, string(ccmapi.Managed),
				"spec", "dependencies", "sailOperator", "managementPolicy",
			)
		}).Eventually().Should(Not(BeNil()))

		wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
			jq.Match(`.metadata.generation > %d`, gen2),
			jq.Match(`.status.observedGeneration == .metadata.generation`),
			jq.Match(`.status.phase == "Ready"`),
		))
	})

	// --- 4. UnmanagedNotReconciled ---
	// Switches cert-manager to Unmanaged, then deletes its deployment and
	// verifies the controller does NOT recreate it. Leaves the CR modified.
	t.Run("UnmanagedNotReconciled", func(t *testing.T) {
		wt := tc.NewWithT(t)

		cr := wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))
		gen, _, _ := unstructured.NestedInt64(cr.Object, "metadata", "generation")

		wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(
				obj.Object, string(ccmapi.Unmanaged),
				"spec", "dependencies", "certManager", "managementPolicy",
			)
		}).Eventually().Should(Not(BeNil()))

		wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
			jq.Match(`.metadata.generation > %d`, gen),
			jq.Match(`.status.observedGeneration == .metadata.generation`),
			jq.Match(`.status.phase == "Ready"`),
		))

		// Delete the cert-manager deployment.
		target := managedDependencyDeployments[0]
		wt.Expect(target.Name).To(ContainSubstring("cert-manager"), "expected first managed deployment to be cert-manager")
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
	})

	// --- 5. GarbageCollection ---
	// Tests the GC action that runs as the last step in the reconciliation pipeline.
	// GC identifies stale or orphaned resources by comparing InstanceGeneration
	// annotations and deletes them, while preserving protected PKI resources.
	t.Run("GarbageCollection", func(t *testing.T) {
		wt := tc.NewWithT(t)

		// Restore all dependencies to Managed (step 4 left cert-manager Unmanaged).
		wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, allManaged(), "spec", "dependencies")
		}).Eventually().Should(Not(BeNil()))

		waitForReady(wt)
		waitForDeploymentsAvailable(wt)

		t.Run("PreservesProtectedPKIResources", func(t *testing.T) {
			wt := tc.NewWithT(t)

			// Trigger a spec change to force a full reconcile (render → deploy → GC).
			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, string(ccmapi.Unmanaged),
					"spec", "dependencies", "sailOperator", "managementPolicy")
			}).Eventually().Should(Not(BeNil()))

			wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(
				jq.Match(`.status.observedGeneration == .metadata.generation`),
			)

			// PKI resources must survive the GC run.
			wt.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{
				Name: pki.IssuerName,
			}).Eventually().Should(Not(BeNil()))
			wt.Get(gvk.CertManagerCertificate, types.NamespacedName{
				Name: pki.CertName, Namespace: pki.CertManagerNamespace,
			}).Eventually().Should(Not(BeNil()))
			wt.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{
				Name: pki.CAIssuerName,
			}).Eventually().Should(Not(BeNil()))

			// Restore sailOperator.
			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, string(ccmapi.Managed),
					"spec", "dependencies", "sailOperator", "managementPolicy")
			}).Eventually().Should(Not(BeNil()))
		})

		t.Run("DeletesStaleResources", func(t *testing.T) {
			wt := tc.NewWithT(t)

			cr := wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))

			// Create a ConfigMap that looks like a stale CCM resource: it has the
			// infrastructure label and an owner reference, but its generation
			// annotation does not match the CR's current generation.
			staleCM := &unstructured.Unstructured{}
			staleCM.SetGroupVersionKind(gvk.ConfigMap)
			staleCM.SetName("stale-ccm-resource")
			staleCM.SetNamespace("cert-manager-operator")
			staleCM.SetLabels(map[string]string{
				labels.InfrastructurePartOf: strings.ToLower(provider.GVK.Kind),
			})
			staleCM.SetAnnotations(map[string]string{
				labels.ODHInfrastructurePrefix + annotations.SuffixInstanceUID:        string(cr.GetUID()),
				labels.ODHInfrastructurePrefix + annotations.SuffixInstanceGeneration: "-1",
			})
			_ = unstructured.SetNestedSlice(staleCM.Object, []any{
				map[string]any{
					"apiVersion": provider.GVK.GroupVersion().String(),
					"kind":       provider.GVK.Kind,
					"name":       provider.InstanceName,
					"uid":        string(cr.GetUID()),
				},
			}, "metadata", "ownerReferences")

			wt.Expect(wt.Client().Create(wt.Context(), staleCM)).To(Succeed())
			t.Cleanup(func() {
				_ = wt.Client().Delete(wt.Context(), staleCM)
			})

			// Trigger a spec change to force a reconcile including GC.
			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, string(ccmapi.Unmanaged),
					"spec", "dependencies", "sailOperator", "managementPolicy")
			}).Eventually().Should(Not(BeNil()))

			// GC should delete the stale resource.
			wt.Get(gvk.ConfigMap, client.ObjectKeyFromObject(staleCM)).Eventually().Should(BeNil())

			// Restore sailOperator.
			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, string(ccmapi.Managed),
					"spec", "dependencies", "sailOperator", "managementPolicy")
			}).Eventually().Should(Not(BeNil()))
		})

		t.Run("DeletesResourcesOnUnmanagedTransition", func(t *testing.T) {
			wt := tc.NewWithT(t)

			waitForReady(wt)

			// Verify the cert-manager deployment exists before the transition.
			certManagerDep := managedDependencyDeployments[0]
			wt.Expect(certManagerDep.Name).To(ContainSubstring("cert-manager"))
			nn := types.NamespacedName{Name: certManagerDep.Name, Namespace: certManagerDep.Namespace}
			wt.Get(gvk.Deployment, nn).Eventually().Should(Not(BeNil()))

			// Switch cert-manager to Unmanaged. Helm no longer renders cert-manager
			// resources, so they retain stale generation annotations. GC deletes them.
			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, string(ccmapi.Unmanaged),
					"spec", "dependencies", "certManager", "managementPolicy")
			}).Eventually().Should(Not(BeNil()))

			// GC should automatically delete the cert-manager deployment.
			wt.Get(gvk.Deployment, nn).Eventually().Should(BeNil())
		})
	})

	// --- 6. CascadeDeletionOnCRDelete ---
	// Deletes the CR and verifies Kubernetes cascade-deletes all owned resources
	// (those with ownerReferences). Namespaces are excluded from ownership and
	// survive deletion. Must be the last test since it destroys the CR.
	t.Run("CascadeDeletionOnCRDelete", func(t *testing.T) {
		wt := tc.NewWithT(t)

		// Restore all dependencies to Managed (previous tests may have changed
		// some to Unmanaged) and wait for all deployments to come back.
		wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, allManaged(), "spec", "dependencies")
		}).Eventually().Should(Not(BeNil()))

		waitForReady(wt)
		waitForDeploymentsAvailable(wt)

		// Verify deployments have owner references pointing to the CR.
		for _, dep := range managedDependencyDeployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.Name, Namespace: dep.Namespace,
			}).Eventually().Should(
				jq.Match(`.metadata.ownerReferences | length > 0`),
			)
		}

		// CertManager/cluster must exist and be owned by this CR.
		wt.Get(gvk.CertManagerV1Alpha1, types.NamespacedName{Name: "cluster"}).
			Eventually().Should(jq.Match(`.metadata.ownerReferences | length > 0`))

		// cert-manager operand Deployments must be running before we delete the CR.
		for _, dep := range certManagerOperandDeployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.Name, Namespace: dep.Namespace,
			}).Eventually().Should(Not(BeNil()))
		}

		// Namespaces are excluded from dynamic ownership to prevent cascade
		// deletion of the entire namespace (and all third-party resources in it)
		// when the CR is deleted. Verify they have no owner references.
		for _, ns := range common.ManagedNamespaces() {
			wt.Get(gvk.Namespace, types.NamespacedName{Name: ns}).
				Eventually().Should(
				jq.Match(`.metadata.ownerReferences == null or (.metadata.ownerReferences | length == 0)`),
			)
		}

		// Delete the CR.
		wt.Delete(provider.GVK, k8sEngineCrNn()).Eventually().Should(Succeed())
		wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(BeNil())

		// All owned deployments should be cascade-deleted via owner references.
		for _, dep := range managedDependencyDeployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.Name, Namespace: dep.Namespace,
			}).Eventually().Should(BeNil())
		}

		// CertManager/cluster CR must be deleted. The finalizer action deletes it
		// to allow cert-manager-operator to clean up its own resources.
		wt.Get(gvk.CertManagerV1Alpha1, types.NamespacedName{Name: "cluster"}).
			Eventually().Should(BeNil())

		// cert-manager operand Deployments must be removed by the cert-manager-operator
		// finalizer. They are not directly owned by our CR, so cascade deletion
		// does not cover them.
		for _, dep := range certManagerOperandDeployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.Name, Namespace: dep.Namespace,
			}).Eventually().Should(BeNil())
		}

		// Namespaces survive CR deletion because they have no owner references.
		for _, ns := range common.ManagedNamespaces() {
			wt.Get(gvk.Namespace, types.NamespacedName{Name: ns}).
				Eventually().Should(Not(BeNil()))
		}
	})
}
