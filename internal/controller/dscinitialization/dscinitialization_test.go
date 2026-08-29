package dscinitialization_test

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	userv1 "github.com/openshift/api/user/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	workingNamespace     = "test-operator-ns"
	applicationName      = "default-dsci"
	customizedAppNs      = "my-opendatahub"
	applicationNamespace = "test-application-ns"
	usergroupName        = "odh-admins"
	monitoringNamespace  = "test-monitoring-ns"
	readyPhase           = "Ready"
)

var _ = Describe("DataScienceCluster initialization", func() {
	Context("Creation of related resources", func() {
		// must be default as instance name, or it will break

		BeforeEach(func(ctx context.Context) {
			// when
			foundApplicationNamespace := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: workingNamespace}, foundApplicationNamespace)).ShouldNot(Succeed())
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
		})

		AfterEach(cleanupResources)

		It("Should create default application namespace", func(ctx context.Context) {
			// then
			foundApplicationNamespace := &corev1.Namespace{}
			Eventually(namespaceExists(applicationNamespace, foundApplicationNamespace)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			Expect(foundApplicationNamespace.Name).To(Equal(applicationNamespace))
			Expect(foundApplicationNamespace.Labels).To(HaveKeyWithValue(labels.SecurityEnforce, "baseline"))
		})

		It("Should stay Ready when the Monitoring CRD is not installed", func(ctx context.Context) {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			err := k8sClient.Get(ctx, client.ObjectKey{Name: serviceApi.MonitoringCRDName}, crd)
			if err == nil {
				Skip("Monitoring CRD already present in this envtest")
			}

			monitoringCR := createMonitoringCR()
			err = k8sClient.Get(ctx, client.ObjectKey{Name: serviceApi.MonitoringInstanceName}, monitoringCR)
			Expect(meta.IsNoMatchError(err)).To(BeTrue())
		})

		// Currently commented out in the DSCI reconcile - setting test to Pending
		It("Should create default network policy", func(ctx context.Context) {
			// then
			foundNetworkPolicy := &networkingv1.NetworkPolicy{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, foundNetworkPolicy)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			Expect(foundNetworkPolicy.Name).To(Equal(applicationNamespace))
			Expect(foundNetworkPolicy.Namespace).To(Equal(applicationNamespace))
			Expect(foundNetworkPolicy.Spec.PolicyTypes[0]).To(Equal(networkingv1.PolicyTypeIngress))
		})

		It("Should not create user group when we do not have authentications CR in the cluster", func(ctx context.Context) {
			userGroup := &userv1.Group{}
			Eventually(objectExists(usergroupName, "", userGroup)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeFalse())
		})
	})

	Context("Monitoring Resource", Ordered, func() {
		BeforeAll(func(ctx context.Context) {
			installMonitoringCRD(ctx)
		})
		AfterEach(cleanupResources)
		const monitoringNamespace2 = "test-monitoring-ns2"
		const applicationName = "default-dsci"

		It("Should not create monitoring namespace if monitoring is disabled", func(ctx context.Context) {
			// when
			desiredDsci := createDSCI(operatorv1.Removed, operatorv1.Managed, monitoringNamespace2)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			// then
			foundMonitoringNamespace := &corev1.Namespace{}
			Eventually(namespaceExists(monitoringNamespace2, foundMonitoringNamespace)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeFalse())
		})

		It("Should mirror dependent operator conditions from Monitoring CR to DSCI", func(ctx context.Context) {
			// given
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())

			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			monitoringCR := waitForMonitoringCR(ctx)

			// when - Simulate Monitoring CR getting some conditions
			Expect(setMonitoringConditions(monitoringCR,
				condition("MonitoringStackAvailable", metav1.ConditionTrue, "Ready", "Monitoring stack is ready"),
				condition("ThanosQuerierAvailable", metav1.ConditionFalse, "Degraded", "Thanos querier is failing"),
				condition("UnrelatedCondition", metav1.ConditionFalse, "Failing", "This should not be mirrored"),
			)).To(Succeed())
			Expect(k8sClient.Status().Update(ctx, monitoringCR)).Should(Succeed())

			// then - DSCI should have only relevant conditions mirrored
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: applicationName, Namespace: workingNamespace}, foundDsci)).To(Succeed())
				// Should contain relevant ones
				g.Expect(foundDsci.Status.Conditions).To(ContainElements(
					SatisfyAll(
						HaveField("Type", "MonitoringStackAvailable"),
						HaveField("Status", metav1.ConditionTrue),
					),
					SatisfyAll(
						HaveField("Type", "ThanosQuerierAvailable"),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "Degraded"),
					),
					SatisfyAll(
						HaveField("Type", "MonitoringReady"),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", "Ready"),
					),
				))
				// Should NOT contain the unrelated one
				g.Expect(foundDsci.Status.Conditions).ToNot(ContainElement(
					HaveField("Type", "UnrelatedCondition"),
				))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("Should set Ready condition to False when Monitoring CR Ready condition is False", func(ctx context.Context) {
			// given
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())

			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			monitoringCR := waitForMonitoringCR(ctx)

			// when - Simulate Monitoring CR Getting Ready=False
			Expect(setMonitoringConditions(monitoringCR,
				condition("Ready", metav1.ConditionFalse, "NotReady", "Monitoring stack is not ready"),
			)).To(Succeed())
			Expect(k8sClient.Status().Update(ctx, monitoringCR)).Should(Succeed())

			// then - DSCI should have Ready=False mirrored from Monitoring AND MonitoringReady=True
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: applicationName, Namespace: workingNamespace}, foundDsci)).To(Succeed())
				g.Expect(foundDsci.Status.Conditions).To(ContainElements(
					SatisfyAll(
						HaveField("Type", "Ready"),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "NotReady"),
						HaveField("Message", ContainSubstring("Monitoring stack is not ready")),
					),
					SatisfyAll(
						HaveField("Type", "MonitoringReady"),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", "Ready"),
					),
				))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("Should update DSCI status when Monitoring CR is deleted", func(ctx context.Context) {
			// given
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())

			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			monitoringCR := waitForMonitoringCR(ctx)

			Expect(setMonitoringConditions(monitoringCR,
				condition("Ready", metav1.ConditionTrue, "Ready", "Monitoring stack is ready"),
			)).To(Succeed())
			Expect(k8sClient.Status().Update(ctx, monitoringCR)).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: applicationName}, foundDsci)).To(Succeed())
				g.Expect(foundDsci.Status.Conditions).To(ContainElement(SatisfyAll(
					HaveField("Type", "MonitoringReady"),
					HaveField("Status", metav1.ConditionTrue),
				)))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			// when - DSCI disables monitoring and deletes the Monitoring CR
			Eventually(func() error {
				current := &dsciv2.DSCInitialization{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: applicationName}, current); err != nil {
					return err
				}
				current.Spec.Monitoring.ManagementState = operatorv1.Removed
				return k8sClient.Update(ctx, current)
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: serviceApi.MonitoringInstanceName}, createMonitoringCR())
				return k8serr.IsNotFound(err)
			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())

			// then - DSCI status should reflect it's removed
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: applicationName}, foundDsci)).To(Succeed())
				g.Expect(foundDsci.Status.Conditions).To(ContainElement(SatisfyAll(
					HaveField("Type", "MonitoringReady"),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "Removed"),
				)))
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})
	})

	Context("Handling existing resources", func() {
		AfterEach(cleanupResources)

		It("Should not update namespace if it exists", func(ctx context.Context) {
			anotherNamespace := "test-another-ns"

			// given
			desiredNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: anotherNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, desiredNamespace)).Should(Succeed())
			createdNamespace := &corev1.Namespace{}
			Eventually(namespaceExists(anotherNamespace, createdNamespace)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			// when
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			// then
			foundApplicationNamespace := &corev1.Namespace{}
			Eventually(namespaceExists(anotherNamespace, foundApplicationNamespace)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			Expect(foundApplicationNamespace.Name).To(Equal(createdNamespace.Name))
			Expect(foundApplicationNamespace.UID).To(Equal(createdNamespace.UID))
		})
	})

	Context("Creation of customized related resources", func() {
		BeforeEach(func(ctx context.Context) {
			// when
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: customizedAppNs,
					Labels: map[string]string{
						labels.CustomizedAppNamespace: labels.True,
					},
				},
			})).Should(Succeed())

		})
		AfterEach(cleanupCustomizedResources)

		It("Should have security label and no generated-namespace lable on existing DSCI specified application namespace", func(ctx context.Context) {
			// then
			desiredDsci := createCustomizedDSCI(customizedAppNs)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			appNS := &corev1.Namespace{}
			Eventually(namespaceExists(customizedAppNs, appNS)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			Eventually(func() map[string]string {
				_ = k8sClient.Get(ctx, client.ObjectKey{Name: customizedAppNs}, appNS)
				return appNS.Labels
			}).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(SatisfyAll(
					HaveKeyWithValue(labels.SecurityEnforce, "baseline"),
					HaveKeyWithValue(labels.CustomizedAppNamespace, labels.True),
					Not(HaveKey(labels.ODH.OwnedNamespace)),
				))
		})
	})

	Context("PSA label preservation", func() {
		const privilegedAppNs = "test-privileged-psa-ns"

		BeforeEach(func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: privilegedAppNs,
					Labels: map[string]string{
						labels.CustomizedAppNamespace: labels.True,
						labels.SecurityEnforce:        "privileged",
					},
					Annotations: map[string]string{
						annotations.PSAElevatedBy: "kserve-modelcache",
					},
				},
			})).Should(Succeed())
		})

		AfterEach(func(ctx context.Context) {
			Expect(k8sClient.DeleteAllOf(ctx, &dsciv2.DSCInitialization{})).To(Succeed())
			Eventually(noInstanceExistsIn(workingNamespace, &dsciv2.DSCInitializationList{})).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			Eventually(func() error {
				appNs := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: privilegedAppNs}, appNs); err != nil {
					return err
				}
				delete(appNs.Labels, labels.CustomizedAppNamespace)
				return k8sClient.Update(ctx, appNs)
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: privilegedAppNs,
				},
			})).To(Succeed())
		})

		It("Should not downgrade privileged PSA label to baseline", func(ctx context.Context) {
			desiredDsci := &dsciv2.DSCInitialization{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DSCInitialization",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      applicationName,
					Namespace: workingNamespace,
				},
				Spec: dsciv2.DSCInitializationSpec{
					ApplicationsNamespace: privilegedAppNs,
					Monitoring: serviceApi.DSCIMonitoring{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
						MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
							Namespace: monitoringNamespace,
						},
					},
					TrustedCABundle: &dsciv2.TrustedCABundleSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			}
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())

			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			appNS := &corev1.Namespace{}
			Eventually(func() map[string]string {
				_ = k8sClient.Get(ctx, client.ObjectKey{Name: privilegedAppNs}, appNS)
				return appNS.Labels
			}).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(SatisfyAll(
					HaveKeyWithValue(labels.SecurityEnforce, "privileged"),
					HaveKeyWithValue(labels.CustomizedAppNamespace, labels.True),
					Not(HaveKey(labels.ODH.OwnedNamespace)),
				))
		})
	})
})

func cleanupCustomizedResources(ctx context.Context) {
	Expect(k8sClient.DeleteAllOf(ctx, &dsciv2.DSCInitialization{})).To(Succeed())
	Eventually(noInstanceExistsIn(customizedAppNs, &dsciv2.DSCInitializationList{})).
		WithContext(ctx).
		WithTimeout(timeout).
		WithPolling(interval).
		Should(BeTrue())

	Eventually(func() error {
		appNs := &corev1.Namespace{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: customizedAppNs}, appNs); err != nil {
			return err
		}
		// Remove special customized label
		delete(appNs.Labels, labels.CustomizedAppNamespace)
		return k8sClient.Update(ctx, appNs)
	}, timeout, interval).Should(Succeed(), "Failed to remove application-namespace label from namespace")

	Expect(k8sClient.Delete(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: customizedAppNs,
		},
	})).To(Succeed())
}

func cleanupResources(ctx context.Context) {
	defaultNamespace := client.InNamespace(workingNamespace)
	appNamespace := client.InNamespace(applicationNamespace)
	Expect(k8sClient.DeleteAllOf(ctx, &dsciv2.DSCInitialization{}, defaultNamespace)).To(Succeed())
	if err := k8sClient.DeleteAllOf(ctx, resources.GvkToUnstructured(gvk.Monitoring)); err != nil {
		Expect(meta.IsNoMatchError(err) || k8serr.IsNotFound(err)).To(BeTrue())
	}

	Expect(k8sClient.DeleteAllOf(ctx, &networkingv1.NetworkPolicy{}, appNamespace)).To(Succeed())
	Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, appNamespace)).To(Succeed())

	Eventually(noInstanceExistsIn(workingNamespace, &dsciv2.DSCInitializationList{})).
		WithContext(ctx).
		WithTimeout(timeout).
		WithPolling(interval).
		Should(BeTrue())
	Eventually(noInstanceExistsIn(applicationNamespace, &corev1.ConfigMapList{})).
		WithContext(ctx).
		WithContext(ctx).
		WithTimeout(timeout).
		WithPolling(interval).
		Should(BeTrue())
}

func noInstanceExistsIn(namespace string, list client.ObjectList) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		if err := k8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
			return false
		}

		return meta.LenList(list) == 0
	}
}

func namespaceExists(ns string, obj client.Object) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: ns}, obj)

		return err == nil
	}
}

func objectExists(name string, namespace string, obj client.Object) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, obj)

		return err == nil
	}
}

func createMonitoringCR() *unstructured.Unstructured {
	u := resources.GvkToUnstructured(gvk.Monitoring)
	u.SetName(serviceApi.MonitoringInstanceName)
	return u
}

func waitForMonitoringCR(ctx context.Context) *unstructured.Unstructured {
	GinkgoHelper()
	monitoringCR := createMonitoringCR()
	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: serviceApi.MonitoringInstanceName}, monitoringCR)
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
	return monitoringCR
}

func condition(condType string, status metav1.ConditionStatus, reason, message string) map[string]any {
	return map[string]any{
		"type":               condType,
		"status":             string(status),
		"reason":             reason,
		"message":            message,
		"lastTransitionTime": metav1.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func setMonitoringConditions(obj *unstructured.Unstructured, conditions ...map[string]any) error {
	raw := make([]any, 0, len(conditions))
	for _, c := range conditions {
		raw = append(raw, c)
	}
	return unstructured.SetNestedSlice(obj.Object, raw, "status", "conditions")
}

func createDSCI(enableMonitoring operatorv1.ManagementState, enableTrustedCABundle operatorv1.ManagementState, monitoringNS string) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      applicationName,
			Namespace: workingNamespace,
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: applicationNamespace,
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{ManagementState: enableMonitoring},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: monitoringNS,
				},
			},
			TrustedCABundle: &dsciv2.TrustedCABundleSpec{
				ManagementState: enableTrustedCABundle,
			},
		},
	}
}

func createCustomizedDSCI(appNS string) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      applicationName,
			Namespace: workingNamespace,
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: appNS,
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: monitoringNamespace,
				},
			},
			TrustedCABundle: &dsciv2.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
			},
		},
	}
}

func dscInitializationIsReady(dsciObj *dsciv2.DSCInitialization) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		_ = k8sClient.Get(ctx, client.ObjectKey{Name: applicationName, Namespace: workingNamespace}, dsciObj)

		return dsciObj.Status.Phase == readyPhase
	}
}
