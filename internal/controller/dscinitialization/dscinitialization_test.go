package dscinitialization_test

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	userv1 "github.com/openshift/api/user/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

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
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci)).
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

	Context("Monitoring Resource", func() {
		AfterEach(cleanupResources)
		const monitoringNamespace2 = "test-monitoring-ns2"
		const applicationName = "default-dsci"

		It("Should not create monitoring namespace if monitoring is disabled", func(ctx context.Context) {
			// when
			desiredDsci := createDSCI(operatorv1.Removed, operatorv1.Managed, monitoringNamespace2)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv2.DSCInitialization{}
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci)).
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

			// Wait for DSCI to be ready and Monitoring CR to be created
			monitoringCR := &serviceApi.Monitoring{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "default-monitoring"}, monitoringCR)
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			// when - Simulate Monitoring CR getting some conditions
			monitoringCR.Status.Conditions = []common.Condition{
				{
					Type:               "MonitoringStackAvailable",
					Status:             metav1.ConditionTrue,
					Reason:             "Ready",
					Message:            "Monitoring stack is ready",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               "ThanosQuerierAvailable",
					Status:             metav1.ConditionFalse,
					Reason:             "Degraded",
					Message:            "Thanos querier is failing",
					LastTransitionTime: metav1.Now(),
				},
				{
					Type:               "UnrelatedCondition",
					Status:             metav1.ConditionFalse,
					Reason:             "Failing",
					Message:            "This should not be mirrored",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, monitoringCR)).Should(Succeed())

			// then - DSCI should have only relevant conditions mirrored
			foundDsci := &dsciv2.DSCInitialization{}
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

			// Wait for DSCI to be ready and Monitoring CR to be created
			monitoringCR := &serviceApi.Monitoring{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "default-monitoring"}, monitoringCR)
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			// when - Simulate Monitoring CR Getting Ready=False
			monitoringCR.Status.Conditions = []common.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "NotReady",
					Message:            "Monitoring stack is not ready",
					LastTransitionTime: metav1.Now(),
				},
			}
			Expect(k8sClient.Status().Update(ctx, monitoringCR)).Should(Succeed())

			// then - DSCI should have Ready=False mirrored from Monitoring AND MonitoringReady=True
			foundDsci := &dsciv2.DSCInitialization{}
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

			monitoringCR := &serviceApi.Monitoring{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "default-monitoring"}, monitoringCR)
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			// when - Monitoring is set to Removed in DSCI, which should delete the Monitoring CR
			foundDsci := &dsciv2.DSCInitialization{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: applicationName, Namespace: workingNamespace}, foundDsci)).To(Succeed())
			foundDsci.Spec.Monitoring.ManagementState = operatorv1.Removed
			Expect(k8sClient.Update(ctx, foundDsci)).To(Succeed())

			// then - Monitoring CR should be gone
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "default-monitoring"}, monitoringCR)
				return k8serr.IsNotFound(err) && err != nil
			}).WithTimeout(timeout).WithPolling(interval).Should(BeTrue())

			// then - DSCI status should reflect it's removed
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: applicationName, Namespace: workingNamespace}, foundDsci)).To(Succeed())
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
		const applicationName = "default-dsci"

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
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci)).
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

func dscInitializationIsReady(name string, namespace string, dsciObj *dsciv2.DSCInitialization) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		_ = k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, dsciObj)

		return dsciObj.Status.Phase == readyPhase
	}
}
