package cloudmanager_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmapi "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
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
		"dependencies": depsWithCustomNamespaces(ccmapi.Managed),
	}

	err := wt.Client().Create(wt.Context(), cr)
	wt.Expect(err).To(HaveOccurred())
}

// TestCloudManager runs all cloud manager e2e tests sequentially under a single
// CR lifecycle. Tests are ordered so that the CR is created once and reused as
// long as possible, minimizing repeated create/teardown cycles.
// CascadeDeletionOnCRDelete must be last since it destroys the CR.
func TestCloudManager(t *testing.T) { //nolint:maintidx // sequential subtests sharing one CR lifecycle are clearer inline
	wt := tc.NewWithT(t)

	cr := newCloudManagerCR(depsWithCustomNamespaces(ccmapi.Managed))
	wt.Create(cr, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))

	// Safety net: if any test fails before GarbageCollectionOnDelete runs,
	// clean up the CR so the next local run starts fresh.
	t.Cleanup(func() {
		_ = wt.Client().Delete(wt.Context(), newCloudManagerCR(depsWithCustomNamespaces(ccmapi.Managed)))
	})

	waitForReady(wt)

	// Read namespace values from the CR spec (custom namespaces configured in depsWithCustomNamespaces).
	crObj := wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))
	deployments := getManagedDependencyDeployments(wt, crObj)
	certManagerOperandNS := getCertManagerOperandNamespace()
	sailOperatorNS := getSailOperatorNamespace(wt, crObj)
	lwsNS := getLWSNamespace(wt, crObj)

	// --- 1. DeploymentsAvailable ---
	// Verifies that dependency namespaces and deployments are created,
	// with deployments reaching Available status.
	t.Run("DeploymentsAvailable", func(t *testing.T) {
		wt := tc.NewWithT(t)

		namespaces := getAllManagedNamespaces(wt)

		for _, ns := range namespaces {
			nsName := ns.GetName()
			t.Run("namespace/"+nsName, func(t *testing.T) {
				wt := tc.NewWithT(t)
				wt.Get(gvk.Namespace, types.NamespacedName{Name: nsName}).
					Eventually().
					Should(Not(BeNil()))
			})
		}

		waitForDeploymentsAvailable(wt, deployments)

		t.Run("cert-manager operand namespace", func(t *testing.T) {
			wt := tc.NewWithT(t)
			wt.Get(gvk.Namespace, types.NamespacedName{Name: certManagerOperandNS}).
				Eventually().
				Should(Not(BeNil()))
		})

		t.Run("cert-manager operand available", func(t *testing.T) {
			wt := tc.NewWithT(t)
			waitForCertManagerOperandAvailable(wt)
		})
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

			t.Run("Dependency deployments running", func(t *testing.T) {
				wt := tc.NewWithT(t)

				for _, ns := range []string{certManagerOperatorNS, customLWSOperatorNS, customSailOperatorNS} {
					wt.List(gvk.Deployment,
						client.InNamespace(ns),
						client.MatchingLabels{labels.InfrastructurePartOf: getPartOfLabelValue()},
					).Eventually().Should(
						jq.Match(`(. | length) > 0 and all(.status.readyReplicas == .status.replicas)`),
						"all deployments in %s should have ready replicas", ns,
					)
				}
			})

			t.Run("Per-dependency conditions", func(t *testing.T) {
				wt := tc.NewWithT(t)

				wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
					jq.Match(`.status.conditions[] | select(.type == "DependenciesReady") | .status == "True"`),
					jq.Match(`.status.conditions[] | select(.type == "GatewayAPIReady") | .status == "True"`),
					jq.Match(`.status.conditions[] | select(.type == "CertManagerReady") | .status == "True"`),
					jq.Match(`.status.conditions[] | select(.type == "LWSReady") | .status == "True"`),
					jq.Match(`.status.conditions[] | select(.type == "SailOperatorReady") | .status == "True"`),
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

		t.Run("CustomNamespacesRespected", func(t *testing.T) {
			t.Run("deployments in custom namespaces", func(t *testing.T) {
				wt := tc.NewWithT(t)

				// Verify all managed deployments are in the expected namespaces
				// (custom for LWS/Sail, hardcoded for cert-manager)
				for _, dep := range deployments {
					ns := dep.GetNamespace()
					wt.Expect(ns).To(SatisfyAny(
						Equal(certManagerOperatorNS),
						Equal(customLWSOperatorNS),
						Equal(customSailOperatorNS),
					), "deployment %s should be in one of the expected namespaces", dep.GetName())
				}
			})
		})

		t.Run("HelmRenderedResources", func(t *testing.T) {
			partOfValue := getPartOfLabelValue()

			for _, dep := range deployments {
				t.Run(dep.GetName(), func(t *testing.T) {
					wt := tc.NewWithT(t)
					nn := types.NamespacedName{Name: dep.GetName(), Namespace: dep.GetNamespace()}

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
			for _, dep := range deployments {
				t.Run(dep.GetName(), func(t *testing.T) {
					wt := tc.NewWithT(t)
					wt.List(gvk.ServiceAccount,
						client.InNamespace(dep.GetNamespace()),
						client.MatchingLabels{labels.InfrastructurePartOf: getPartOfLabelValue()},
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
				wt.Get(ccmcommon.LWSOperatorCR.GVK, types.NamespacedName{
					Name: ccmcommon.LWSOperatorCR.Name,
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
				wt.Get(ccmcommon.SailOperatorCR.GVK, types.NamespacedName{
					Name: ccmcommon.SailOperatorCR.Name, Namespace: sailOperatorNS,
				}).Eventually().Should(
					jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
				)
			})
		})

		// DeploymentSelfHealing: deletes each managed deployment and verifies the
		// controller recreates it with a new UID. The engine CR itself is not mutated.
		t.Run("DeploymentSelfHealing", func(t *testing.T) {
			for _, dep := range deployments {
				t.Run(dep.GetName(), func(t *testing.T) {
					wt := tc.NewWithT(t)
					nn := types.NamespacedName{Name: dep.GetName(), Namespace: dep.GetNamespace()}

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

	// --- 3. NamespaceImmutability ---
	// Verifies that namespace fields in dependency configurations are immutable
	// once set. The CEL validation rules should reject any attempts to change
	// namespace values after creation.
	t.Run("NamespaceImmutability", func(t *testing.T) {
		t.Run("lws namespace is immutable", func(t *testing.T) {
			wt := tc.NewWithT(t)

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(provider.GVK)
			err := wt.Client().Get(wt.Context(), k8sEngineCrNn(), obj)
			wt.Expect(err).NotTo(HaveOccurred())

			err = unstructured.SetNestedField(
				obj.Object, "modified-lws-operator",
				"spec", "dependencies", "lws", "configuration", "namespace",
			)
			wt.Expect(err).NotTo(HaveOccurred(), "failed to set nested field")

			err = wt.Client().Update(wt.Context(), obj)
			wt.Expect(err).To(HaveOccurred(), "changing lws namespace should be rejected")
			wt.Expect(err.Error()).To(ContainSubstring("immutable"), "error should mention immutability")
		})

		t.Run("sailOperator namespace is immutable", func(t *testing.T) {
			wt := tc.NewWithT(t)

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(provider.GVK)
			err := wt.Client().Get(wt.Context(), k8sEngineCrNn(), obj)
			wt.Expect(err).NotTo(HaveOccurred())

			err = unstructured.SetNestedField(
				obj.Object, "modified-istio-system",
				"spec", "dependencies", "sailOperator", "configuration", "namespace",
			)
			wt.Expect(err).NotTo(HaveOccurred(), "failed to set nested field")

			err = wt.Client().Update(wt.Context(), obj)
			wt.Expect(err).To(HaveOccurred(), "changing sailOperator namespace should be rejected")
			wt.Expect(err.Error()).To(ContainSubstring("immutable"), "error should mention immutability")
		})

		t.Run("namespaces remain unchanged after rejected updates", func(t *testing.T) {
			wt := tc.NewWithT(t)

			// Verify that custom namespaces still have their original values
			// after all the rejected update attempts above.
			// Note: cert-manager uses hardcoded namespaces and has no configuration section.
			wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(And(
				jq.Match(`.spec.dependencies.lws.configuration.namespace == "%s"`, customLWSOperatorNS),
				jq.Match(`.spec.dependencies.sailOperator.configuration.namespace == "%s"`, customSailOperatorNS),
			))
		})
	})

	// --- 4. StatusAfterSpecChange ---
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
			jq.Match(`.status.conditions[] | select(.type == "SailOperatorReady") | .reason == "Unmanaged"`),
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
			jq.Match(`.status.conditions[] | select(.type == "SailOperatorReady") | .status == "True"`),
			jq.Match(`.status.conditions[] | select(.type == "SailOperatorReady") | .reason != "Unmanaged"`),
		))
	})

	// --- 5. GarbageCollection ---
	// Tests the GC action that runs as the last step in the reconciliation pipeline.
	// GC identifies stale or orphaned resources by comparing InstanceGeneration
	// annotations and deletes them, while preserving protected PKI resources.
	t.Run("GarbageCollection", func(t *testing.T) {
		wt := tc.NewWithT(t)

		// Restore all dependencies to Managed (step 6 already restored all to Managed,
		// but this ensures a clean state regardless of earlier test mutations).
		wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, depsWithCustomNamespaces(ccmapi.Managed), "spec", "dependencies")
		}).Eventually().Should(Not(BeNil()))

		waitForReady(wt)
		waitForDeploymentsAvailable(wt, deployments)

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

			// Restore all to Managed and wait for everything to be deployed.
			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, depsWithCustomNamespaces(ccmapi.Managed), "spec", "dependencies")
			}).Eventually().Should(Not(BeNil()))

			waitForReady(wt)
			waitForDeploymentsAvailable(wt, deployments)

			// Capture deployment references BEFORE the transition (they'll be deleted after).
			// Use a fresh CR from the cluster to ensure current spec values.
			freshCR := wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(Not(BeNil()))

			certManagerDep := getManagedDependencyDeploymentByName(wt, freshCR, "cert-manager")
			certManagerNN := types.NamespacedName{Name: certManagerDep.GetName(), Namespace: certManagerDep.GetNamespace()}

			lwsDep := getManagedDependencyDeploymentByName(wt, freshCR, "lws")
			lwsNN := types.NamespacedName{Name: lwsDep.GetName(), Namespace: lwsDep.GetNamespace()}

			sailDep := getManagedDependencyDeploymentByName(wt, freshCR, "servicemesh")
			sailNN := types.NamespacedName{Name: sailDep.GetName(), Namespace: sailDep.GetNamespace()}

			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: "istiod", Namespace: sailOperatorNS,
			}).Eventually().Should(Not(BeNil()))

			// TODO(RHOAIENG-68252): cert-manager must be transitioned separately because
			// its webhook may block SSA-apply of PKI resources during Phase 2 cleanup.
			// Once the cross-dependency is resolved, all deps can transition together.

			// --- Phase A: switch sail-operator, LWS, gatewayAPI to Unmanaged ---
			wt.Log("Phase A: switching sail-operator, LWS, gatewayAPI to Unmanaged")

			allUnmanaged := depsWithCustomNamespaces(ccmapi.Unmanaged)
			allUnmanaged["certManager"] = map[string]any{"managementPolicy": string(ccmapi.Managed)}

			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, allUnmanaged, "spec", "dependencies")
			}).Eventually().Should(Not(BeNil()))

			// sail-operator cleanup check.
			// Istio CR is deleted first (foreground propagation), forcing sail-operator to
			// process the IstioRevision finalizer before cascade proceeds. IstioRevision
			// must be fully gone — a stuck entry here means istiod would be orphaned.
			wt.Get(ccmcommon.SailOperatorCR.GVK, types.NamespacedName{
				Name: ccmcommon.SailOperatorCR.Name, Namespace: sailOperatorNS,
			}).Eventually().Should(BeNil())

			wt.List(gvk.IstioRevision).Eventually().Should(BeEmpty())

			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: "istiod", Namespace: sailOperatorNS,
			}).Eventually().Should(BeNil())

			wt.Get(gvk.Deployment, sailNN).Eventually().Should(BeNil())

			// LWS operator cleanup check
			wt.Get(gvk.Deployment, lwsNN).Eventually().Should(BeNil())
			wt.Get(ccmcommon.LWSOperatorCR.GVK, types.NamespacedName{
				Name: ccmcommon.LWSOperatorCR.Name, Namespace: lwsNS,
			}).Eventually().Should(BeNil())

			// --- Phase B: switch cert-manager to Unmanaged separately (CM-1019) ---
			wt.Log("Phase B: switching cert-manager to Unmanaged")

			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, string(ccmapi.Unmanaged),
					"spec", "dependencies", "certManager", "managementPolicy")
			}).Eventually().Should(Not(BeNil()))

			// cert-manager: operator deployment removed by cleanup.
			wt.Get(gvk.Deployment, certManagerNN).Eventually().Should(BeNil())

			// CM-1019: cert-manager-operator may recreate CertManager/cluster during
			// graceful shutdown and die before processing its finalizers, leaving
			// operand deployments orphaned. Force-clean the CR and operands.
			forceDeleteCertManagerCR(wt)
			wt.DeleteAll(gvk.Deployment, client.InNamespace(certManagerOperandNS)).Eventually().Should(Succeed())
			wt.List(gvk.Deployment, client.InNamespace(certManagerOperandNS)).Eventually().Should(BeEmpty())

			// All managed deployments should be gone.
			wt.Log("verifying all managed deployments are gone")
			wt.List(gvk.Deployment,
				client.MatchingLabels{labels.InfrastructurePartOf: getPartOfLabelValue()},
			).Eventually().Should(BeEmpty())

			// Restore all to Managed for subsequent tests.
			wt.Log("restoring all dependencies to Managed")
			wt.Patch(provider.GVK, k8sEngineCrNn(), func(obj *unstructured.Unstructured) error {
				return unstructured.SetNestedField(obj.Object, depsWithCustomNamespaces(ccmapi.Managed), "spec", "dependencies")
			}).Eventually().Should(Not(BeNil()))

			waitForReady(wt)
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
			return unstructured.SetNestedField(obj.Object, depsWithCustomNamespaces(ccmapi.Managed), "spec", "dependencies")
		}).Eventually().Should(Not(BeNil()))

		waitForReady(wt)
		waitForDeploymentsAvailable(wt, deployments)

		// Verify deployments have owner references pointing to the CR.
		for _, dep := range deployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.GetName(), Namespace: dep.GetNamespace(),
			}).Eventually().Should(
				jq.Match(`.metadata.ownerReferences | length > 0`),
			)
		}

		// CertManager/cluster must exist and be owned by this CR.
		wt.Get(ccmcommon.CertManagerOperatorCR.GVK, types.NamespacedName{Name: ccmcommon.CertManagerOperatorCR.Name}).
			Eventually().Should(jq.Match(`.metadata.ownerReferences | length > 0`))

		// cert-manager operand Deployments must be running before we delete the CR.
		for _, dep := range certManagerOperandDeployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.Name, Namespace: dep.Namespace,
			}).Eventually().Should(Not(BeNil()))
		}

		// Verify namespaces have owner references pointing to the CR.
		namespaces := getAllManagedNamespaces(wt)

		// Namespaces are excluded from dynamic ownership to prevent cascade
		// deletion of the entire namespace (and all third-party resources in it)
		// when the CR is deleted. Verify they have no owner references.
		for _, ns := range namespaces {
			wt.Get(gvk.Namespace, types.NamespacedName{Name: ns.GetName()}).
				Eventually().Should(
				jq.Match(`.metadata.ownerReferences == null or (.metadata.ownerReferences | length == 0)`),
			)
		}

		// Delete the CR.
		wt.Delete(provider.GVK, k8sEngineCrNn()).Eventually().Should(Succeed())
		wt.Get(provider.GVK, k8sEngineCrNn()).Eventually().Should(BeNil())

		// The finalizer on the KE CR must delete Istio CR first
		// (foreground propagation), forcing sail-operator to process the IstioRevision
		// finalizer before cascade proceeds. A stuck IstioRevision would leave istiod
		// orphaned after CR deletion.
		wt.List(gvk.IstioRevision).Eventually().Should(BeEmpty())
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "istiod", Namespace: sailOperatorNS,
		}).Eventually().Should(BeNil())

		// All owned deployments should be garbage-collected.
		for _, dep := range deployments {
			wt.Get(gvk.Deployment, types.NamespacedName{
				Name: dep.GetName(), Namespace: dep.GetNamespace(),
			}).Eventually().Should(BeNil())
		}

		// FIXME(CM-1019, RHOAIENG-68252): CertManager/cluster CR may be recreated by the dying
		// cert-manager-operator. Skipped until the operator stops auto-recreating it or
		// cert-manager is removed from the CCM managed dependencies.
		// wt.Get(ccmcommon.CertManagerOperatorCR.GVK, types.NamespacedName{Name: ccmcommon.CertManagerOperatorCR.Name}).
		// 	Eventually().Should(BeNil())
		// cert-manager operand Deployments must be removed by the cert-manager-operator
		// finalizer. They are not directly owned by our CR, so cascade deletion
		// does not cover them.
		// for _, dep := range certManagerOperandDeployments {
		// 	wt.Get(gvk.Deployment, types.NamespacedName{
		// 		Name: dep.Name, Namespace: dep.Namespace,
		// 	}).Eventually().Should(BeNil())
		// }

		// Namespaces survive CR deletion because they have no owner references.
		for _, ns := range namespaces {
			wt.Get(gvk.Namespace, types.NamespacedName{Name: ns.GetName()}).
				Eventually().Should(Not(BeNil()))
		}
	})
}
