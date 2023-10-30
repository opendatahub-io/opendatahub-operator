package dscinitialization_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	workingNamespace     = "default"
	applicationNamespace = "test-application-ns"
	monitoringNamespace  = "test-monitoring-ns"
	configmapName        = "odh-common-config"
	readyPhase           = "Ready"
)

var _ = Describe("DataScienceCluster initialization", func() {
	Context("Creation of related resources", func() {
		applicationName := "default-test"
		BeforeEach(func() {
			// when
			desiredDsci := createDSCI(applicationName)
			Expect(k8sClient.Create(context.Background(), desiredDsci)).Should(Succeed())
			foundDsci := &dsci.DSCInitialization{}
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci), timeout, interval).Should(BeTrue())
		})

		AfterEach(cleanupResources)

		It("Should create default application namespace", func() {
			// then
			foundApplicationNamespace := &corev1.Namespace{}
			Eventually(namespaceExists(applicationNamespace, foundApplicationNamespace), timeout, interval).Should(BeTrue())
			Expect(foundApplicationNamespace.Name).To(Equal(applicationNamespace))
		})

		It("Should create default monitoring namespace", func() {
			// then
			foundMonitoringNamespace := &corev1.Namespace{}
			Eventually(Eventually(namespaceExists(monitoringNamespace, foundMonitoringNamespace), timeout, interval).Should(BeTrue()), timeout, interval).Should(BeTrue())
			Expect(foundMonitoringNamespace.Name).Should(Equal(monitoringNamespace))
		})

		// Currently commented out in the DSCI reconcile - setting test to Pending
		It("Should create default network policy", func() {
			// then
			foundNetworkPolicy := &netv1.NetworkPolicy{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, foundNetworkPolicy), timeout, interval).Should(BeTrue())
			Expect(foundNetworkPolicy.Name).To(Equal(applicationNamespace))
			Expect(foundNetworkPolicy.Namespace).To(Equal(applicationNamespace))
			Expect(foundNetworkPolicy.Spec.PolicyTypes[0]).To(Equal(netv1.PolicyTypeIngress))
		})

		It("Should create default rolebinding", func() {
			// then
			foundRoleBinding := &authv1.RoleBinding{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, foundRoleBinding), timeout, interval).Should(BeTrue())
			expectedSubjects := []authv1.Subject{
				{
					Kind:      "ServiceAccount",
					Namespace: applicationNamespace,
					Name:      workingNamespace,
				},
			}
			expectedRoleRef := authv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "system:openshift:scc:anyuid",
			}
			Expect(foundRoleBinding.Name).To(Equal(applicationNamespace))
			Expect(foundRoleBinding.Namespace).To(Equal(applicationNamespace))
			Expect(foundRoleBinding.Subjects).To(Equal(expectedSubjects))
			Expect(foundRoleBinding.RoleRef).To(Equal(expectedRoleRef))
		})

		It("Should create default configmap", func() {
			// then
			foundConfigMap := &corev1.ConfigMap{}
			Eventually(objectExists(configmapName, applicationNamespace, foundConfigMap), timeout, interval).Should(BeTrue())
			Expect(foundConfigMap.Name).To(Equal(configmapName))
			Expect(foundConfigMap.Namespace).To(Equal(applicationNamespace))
			expectedConfigmapData := map[string]string{"namespace": applicationNamespace}
			Expect(foundConfigMap.Data).To(Equal(expectedConfigmapData))
		})
	})

	Context("Handling existing resources", func() {
		AfterEach(cleanupResources)

		It("Should not update rolebinding if it exists", func() {
			applicationName := "rolebinding-test"

			// given
			desiredRoleBinding := &authv1.RoleBinding{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RoleBinding",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      applicationNamespace,
					Namespace: applicationNamespace,
				},

				RoleRef: authv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "system:openshift:scc:anyuid",
				},
			}
			Expect(k8sClient.Create(context.Background(), desiredRoleBinding)).Should(Succeed())
			createdRoleBinding := &authv1.RoleBinding{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, createdRoleBinding), timeout, interval).Should(BeTrue())

			// when
			desiredDsci := createDSCI(applicationName)
			Expect(k8sClient.Create(context.Background(), desiredDsci)).Should(Succeed())
			foundDsci := &dsci.DSCInitialization{}
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci), timeout, interval).Should(BeTrue())

			// then
			foundRoleBinding := &authv1.RoleBinding{}
			Eventually(objectExists(applicationNamespace, applicationNamespace, foundRoleBinding), timeout, interval).Should(BeTrue())
			Expect(foundRoleBinding.UID).To(Equal(createdRoleBinding.UID))
			Expect(foundRoleBinding.Subjects).To(BeNil())
		})

		It("Should not update configmap if it exists", func() {
			applicationName := "configmap-test"

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
			Expect(k8sClient.Create(context.Background(), desiredConfigMap)).Should(Succeed())
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(objectExists(configmapName, applicationNamespace, createdConfigMap), timeout, interval).Should(BeTrue())

			// when
			desiredDsci := createDSCI(applicationName)
			Expect(k8sClient.Create(context.Background(), desiredDsci)).Should(Succeed())
			foundDsci := &dsci.DSCInitialization{}
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci), timeout, interval).Should(BeTrue())

			// then
			foundConfigMap := &corev1.ConfigMap{}
			Eventually(objectExists(configmapName, applicationNamespace, foundConfigMap), timeout, interval).Should(BeTrue())
			Expect(foundConfigMap.UID).To(Equal(createdConfigMap.UID))
			Expect(foundConfigMap.Data).To(Equal(map[string]string{"namespace": "existing-data"}))
			Expect(foundConfigMap.Data).ToNot(Equal(map[string]string{"namespace": applicationNamespace}))
		})

		It("Should not update namespace if it exists", func() {
			applicationName := "configmap-test"
			anotherNamespace := "test-another-ns"

			// given
			desiredNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: anotherNamespace,
				},
			}
			Expect(k8sClient.Create(context.Background(), desiredNamespace)).Should(Succeed())
			createdNamespace := &corev1.Namespace{}
			Eventually(namespaceExists(anotherNamespace, createdNamespace), timeout, interval).Should(BeTrue())

			// when
			desiredDsci := createDSCI(applicationName)
			Expect(k8sClient.Create(context.Background(), desiredDsci)).Should(Succeed())
			foundDsci := &dsci.DSCInitialization{}
			Eventually(dscInitializationIsReady(applicationName, workingNamespace, foundDsci), timeout, interval).Should(BeTrue())

			// then
			foundApplicationNamespace := &corev1.Namespace{}
			Eventually(namespaceExists(anotherNamespace, foundApplicationNamespace), timeout, interval).Should(BeTrue())
			Expect(foundApplicationNamespace.Name).To(Equal(createdNamespace.Name))
			Expect(foundApplicationNamespace.UID).To(Equal(createdNamespace.UID))
		})
	})
})

// cleanup utility func
func cleanupResources() {
	defaultNamespace := client.InNamespace(workingNamespace)
	appNamespace := client.InNamespace(applicationNamespace)
	Expect(k8sClient.DeleteAllOf(context.TODO(), &dsci.DSCInitialization{}, defaultNamespace)).To(Succeed())
	Expect(k8sClient.DeleteAllOf(context.TODO(), &netv1.NetworkPolicy{}, appNamespace)).To(Succeed())
	Expect(k8sClient.DeleteAllOf(context.TODO(), &corev1.ConfigMap{}, appNamespace)).To(Succeed())
	Expect(k8sClient.DeleteAllOf(context.TODO(), &authv1.RoleBinding{}, appNamespace)).To(Succeed())
	Eventually(noInstanceExistsIn(workingNamespace, &dsci.DSCInitializationList{}), timeout, interval).Should(BeTrue())
}

func noInstanceExistsIn(namespace string, list client.ObjectList) func() bool {
	return func() bool {
		if err := k8sClient.List(ctx, list, &client.ListOptions{Namespace: namespace}); err != nil {
			return false
		}

		return meta.LenList(list) == 0
	}
}

func namespaceExists(ns string, obj client.Object) func() bool {
	return func() bool {
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: ns}, obj)
		return err == nil
	}
}

func objectExists(ns string, name string, obj client.Object) func() bool { //nolint
	return func() bool {
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: ns, Namespace: name}, obj)
		return err == nil
	}
}

func createDSCI(appName string) *dsci.DSCInitialization {
	return &dsci.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: workingNamespace,
		},
		Spec: dsci.DSCInitializationSpec{
			ApplicationsNamespace: applicationNamespace,
			Monitoring: dsci.Monitoring{
				Namespace:       monitoringNamespace,
				ManagementState: operatorv1.Managed,
			},
		},
	}
}

func dscInitializationIsReady(ns string, name string, dsciObj *dsci.DSCInitialization) func() bool { //nolint
	return func() bool {
		_ = k8sClient.Get(context.Background(), client.ObjectKey{Name: ns, Namespace: name}, dsciObj)
		return dsciObj.Status.Phase == readyPhase
	}
}
