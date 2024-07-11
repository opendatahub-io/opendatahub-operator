package dscinitialization_test

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	workingNamespace     = "test-operator-ns"
	applicationName      = "default-dsci"
	applicationNamespace = "test-application-ns"
	configmapName        = "odh-common-config"
	monitoringNamespace  = "test-monitoring-ns"
	readyPhase           = "Ready"
)

var _ = Describe("DataScienceCluster initialization", func() {
	Context("Creation of related resources", func() {
		// must be default as instance name, or it will break

		BeforeEach(func(ctx context.Context) {
			// when
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv1.DSCInitialization{}
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

		It("Should create default rolebinding", func(ctx context.Context) {
			// then
			foundRoleBinding := &rbacv1.RoleBinding{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, foundRoleBinding)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			expectedSubjects := []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Namespace: applicationNamespace,
					Name:      "default",
				},
			}
			expectedRoleRef := rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "system:openshift:scc:anyuid",
			}
			Expect(foundRoleBinding.Name).To(Equal(applicationNamespace))
			Expect(foundRoleBinding.Namespace).To(Equal(applicationNamespace))
			Expect(foundRoleBinding.Subjects).To(Equal(expectedSubjects))
			Expect(foundRoleBinding.RoleRef).To(Equal(expectedRoleRef))
		})

		It("Should create default configmap", func(ctx context.Context) {
			// then
			foundConfigMap := &corev1.ConfigMap{}
			Eventually(objectExists(configmapName, applicationNamespace, foundConfigMap)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			Expect(foundConfigMap.Name).To(Equal(configmapName))
			Expect(foundConfigMap.Namespace).To(Equal(applicationNamespace))
			expectedConfigmapData := map[string]string{"namespace": applicationNamespace}
			Expect(foundConfigMap.Data).To(Equal(expectedConfigmapData))
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
			foundDsci := &dsciv1.DSCInitialization{}
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
		It("Should create default monitoring namespace if monitoring enabled", func(ctx context.Context) {
			// when
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace2)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv1.DSCInitialization{}
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
				Should(BeTrue())
			Expect(foundMonitoringNamespace.Name).Should(Equal(monitoringNamespace2))
		})
	})

	Context("Handling existing resources", func() {
		AfterEach(cleanupResources)
		const applicationName = "default-dsci"

		It("Should not update rolebinding if it exists", func(ctx context.Context) {

			// given
			desiredRoleBinding := &rbacv1.RoleBinding{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RoleBinding",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      applicationNamespace,
					Namespace: applicationNamespace,
				},

				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "system:openshift:scc:anyuid",
				},
			}
			Expect(k8sClient.Create(ctx, desiredRoleBinding)).Should(Succeed())
			createdRoleBinding := &rbacv1.RoleBinding{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, createdRoleBinding)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			// when
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv1.DSCInitialization{}
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			// then
			foundRoleBinding := &rbacv1.RoleBinding{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, foundRoleBinding)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			Expect(foundRoleBinding.UID).To(Equal(createdRoleBinding.UID))
			Expect(foundRoleBinding.Subjects).To(BeNil())
		})

		It("Should not update configmap if it exists", func(ctx context.Context) {

			// given
			desiredConfigMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      configmapName,
					Namespace: applicationNamespace,
				},
				Data: map[string]string{"namespace": "existing-data"},
			}
			Expect(k8sClient.Create(ctx, desiredConfigMap)).Should(Succeed())
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(objectExists(configmapName, applicationNamespace, createdConfigMap)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			// when
			desiredDsci := createDSCI(operatorv1.Managed, operatorv1.Managed, monitoringNamespace)
			Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
			foundDsci := &dsciv1.DSCInitialization{}
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())

			// then
			foundConfigMap := &corev1.ConfigMap{}
			Eventually(objectExists(configmapName, applicationNamespace, foundConfigMap)).
				WithContext(ctx).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(BeTrue())
			Expect(foundConfigMap.UID).To(Equal(createdConfigMap.UID))
			Expect(foundConfigMap.Data).To(Equal(map[string]string{"namespace": "existing-data"}))
			Expect(foundConfigMap.Data).ToNot(Equal(map[string]string{"namespace": applicationNamespace}))
		})

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
			foundDsci := &dsciv1.DSCInitialization{}
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
})

func cleanupResources(ctx context.Context) {
	defaultNamespace := client.InNamespace(workingNamespace)
	appNamespace := client.InNamespace(applicationNamespace)
	Expect(k8sClient.DeleteAllOf(ctx, &dsciv1.DSCInitialization{}, defaultNamespace)).To(Succeed())

	Expect(k8sClient.DeleteAllOf(ctx, &networkingv1.NetworkPolicy{}, appNamespace)).To(Succeed())
	Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, appNamespace)).To(Succeed())
	Expect(k8sClient.DeleteAllOf(ctx, &rbacv1.RoleBinding{}, appNamespace)).To(Succeed())
	Expect(k8sClient.DeleteAllOf(ctx, &rbacv1.ClusterRoleBinding{}, appNamespace)).To(Succeed())

	Eventually(noInstanceExistsIn(workingNamespace, &dsciv1.DSCInitializationList{})).
		WithContext(ctx).
		WithTimeout(timeout).
		WithPolling(interval).
		Should(BeTrue())
	Eventually(noInstanceExistsIn(applicationNamespace, &rbacv1.ClusterRoleBindingList{})).
		WithContext(ctx).
		WithTimeout(timeout).
		WithPolling(interval).
		Should(BeTrue())
	Eventually(noInstanceExistsIn(applicationNamespace, &rbacv1.RoleBindingList{})).
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

func objectExists(ns string, name string, obj client.Object) func(ctx context.Context) bool { //nolint:unparam
	return func(ctx context.Context) bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: ns, Namespace: name}, obj)

		return err == nil
	}
}

func createDSCI(enableMonitoring operatorv1.ManagementState, enableTrustedCABundle operatorv1.ManagementState, monitoringNS string) *dsciv1.DSCInitialization {
	return &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      applicationName,
			Namespace: workingNamespace,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: applicationNamespace,
			Monitoring: dsciv1.Monitoring{
				Namespace:       monitoringNS,
				ManagementState: enableMonitoring,
			},
			TrustedCABundle: &dsciv1.TrustedCABundleSpec{
				ManagementState: enableTrustedCABundle,
			},
		},
	}
}

func dscInitializationIsReady(ns string, name string, dsciObj *dsciv1.DSCInitialization) func(ctx context.Context) bool { //nolint:unparam
	return func(ctx context.Context) bool {
		_ = k8sClient.Get(ctx, client.ObjectKey{Name: ns, Namespace: name}, dsciObj)

		return dsciObj.Status.Phase == readyPhase
	}
}
