package servicemesh_test

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	timeout  = 5 * time.Second
	interval = 250 * time.Millisecond
)

var _ = Describe("preconditions", func() {

	Context("namespace existence", func() {

		var (
			objectCleaner *envtestutil.Cleaner
			testFeature   *feature.Feature
			namespace     string
		)

		BeforeEach(func() {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

			testFeatureName := "test-ns-creation"
			namespace = envtestutil.AppendRandomNameTo(testFeatureName)

			dsciSpec := newDSCInitializationSpec(namespace)
			var err error
			testFeature, err = feature.CreateFeature(testFeatureName).
				For(dsciSpec).
				UsingConfig(envTest.Config).
				Load()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create namespace if it does not exist", func() {
			// given
			_, err := getNamespace(namespace)
			Expect(errors.IsNotFound(err)).To(BeTrue())
			defer objectCleaner.DeleteAll(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			err = feature.CreateNamespaceIfNotExists(namespace)(testFeature)

			// then
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not try to create namespace if it does already exist", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			// when
			err := feature.CreateNamespaceIfNotExists(namespace)(testFeature)

			// then
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("ensuring custom resource definitions are installed", func() {

		var (
			dsciSpec            *dscv1.DSCInitializationSpec
			verificationFeature *feature.Feature
		)

		BeforeEach(func() {
			dsciSpec = newDSCInitializationSpec("default")
		})

		It("should successfully check for existing CRD", func() {
			// given example CRD installed into env from /ossm/test/crd/
			name := "test-resources.openshift.io"

			var err error
			verificationFeature, err = feature.CreateFeature("CRD verification").
				For(dsciSpec).
				UsingConfig(envTest.Config).
				PreConditions(feature.EnsureCRDIsInstalled(name)).
				Load()
			Expect(err).ToNot(HaveOccurred())

			// when
			err = verificationFeature.Apply()

			// then
			Expect(err).ToNot(HaveOccurred())
		})

		It("should fail to check non-existing CRD", func() {
			// given
			name := "non-existing-resource.non-existing-group.io"

			var err error
			verificationFeature, err = feature.CreateFeature("CRD verification").
				For(dsciSpec).
				UsingConfig(envTest.Config).
				PreConditions(feature.EnsureCRDIsInstalled(name)).
				Load()
			Expect(err).ToNot(HaveOccurred())

			// when
			err = verificationFeature.Apply()

			// then
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("\"non-existing-resource.non-existing-group.io\" not found"))
		})
	})

})

var _ = Describe("Ensuring service mesh is set up correctly", func() {

	var (
		objectCleaner    *envtestutil.Cleaner
		dsciSpec         *dscv1.DSCInitializationSpec
		serviceMeshSpec  *infrav1.ServiceMeshSpec
		serviceMeshCheck *feature.Feature
		name             = "test-name"
		namespace        = "test-namespace"
	)

	BeforeEach(func() {
		dsciSpec = newDSCInitializationSpec(namespace)
		var err error
		serviceMeshSpec = &dsciSpec.ServiceMesh

		serviceMeshSpec.ControlPlane.Name = name
		serviceMeshSpec.ControlPlane.Namespace = namespace

		serviceMeshCheck, err = feature.CreateFeature("datascience-project-migration").
			For(dsciSpec).
			UsingConfig(envTest.Config).
			PreConditions(servicemesh.EnsureServiceMeshInstalled).Load()

		Expect(err).ToNot(HaveOccurred())

		objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
	})

	It("should find installed Service Mesh Control Plane", func() {
		ns := createNamespace(namespace)
		Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
		defer objectCleaner.DeleteAll(ns)

		createServiceMeshControlPlane(name, namespace)

		// when
		err := serviceMeshCheck.Apply()

		// then
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail to find Service Mesh Control Plane if not present", func() {
		Expect(serviceMeshCheck.Apply()).ToNot(Succeed())
	})

})

var _ = Describe("Data Science Project Migration", func() {

	var (
		objectCleaner    *envtestutil.Cleaner
		dsciSpec         *dscv1.DSCInitializationSpec
		migrationFeature *feature.Feature
	)

	BeforeEach(func() {
		objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

		dsciSpec = newDSCInitializationSpec("default")

		var err error
		migrationFeature, err = feature.CreateFeature("datascience-project-migration").
			For(dsciSpec).
			UsingConfig(envTest.Config).
			WithResources(servicemesh.MigratedDataScienceProjects).Load()

		Expect(err).ToNot(HaveOccurred())

	})

	It("should migrate single namespace", func() {
		// given
		dataScienceNs := createDataScienceProject("dsp-01")
		regularNs := createNamespace("non-dsp")
		Expect(envTestClient.Create(context.Background(), dataScienceNs)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(dataScienceNs, regularNs)

		// when
		Expect(migrationFeature.Apply()).To(Succeed())

		// then
		Eventually(findMigratedNamespaces, timeout, interval).Should(
			And(
				HaveLen(1),
				ContainElement("dsp-01"),
			),
		)
	})

	It("should not migrate any non-datascience namespace", func() {
		// given
		regularNs := createNamespace("non-dsp")
		Expect(envTestClient.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(regularNs)

		// when
		Expect(migrationFeature.Apply()).To(Succeed())

		// then
		Consistently(findMigratedNamespaces, timeout, interval).Should(BeEmpty()) // we can't wait forever, but this should be good enough ;)
	})

	It("should migrate multiple namespaces", func() {
		// given
		dataScienceNs01 := createDataScienceProject("dsp-01")
		dataScienceNs02 := createDataScienceProject("dsp-02")
		dataScienceNs03 := createDataScienceProject("dsp-03")
		regularNs := createNamespace("non-dsp")
		Expect(envTestClient.Create(context.Background(), dataScienceNs01)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), dataScienceNs02)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), dataScienceNs03)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(dataScienceNs01, dataScienceNs02, dataScienceNs03, regularNs)

		// when
		Expect(migrationFeature.Apply()).To(Succeed())

		// then
		Eventually(findMigratedNamespaces, timeout, interval).Should(
			And(
				HaveLen(3),
				ContainElements("dsp-01", "dsp-02", "dsp-03"),
			),
		)
	})

})

var _ = Describe("Cleanup operations", func() {

	Context("configuring control plane for auth(z)", func() {

		var (
			objectCleaner   *envtestutil.Cleaner
			dsciSpec        *dscv1.DSCInitializationSpec
			serviceMeshSpec *infrav1.ServiceMeshSpec
			namespace       = "test"
			name            = "minimal"
		)

		BeforeEach(func() {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

			dsciSpec = newDSCInitializationSpec(namespace)

			serviceMeshSpec = &dsciSpec.ServiceMesh

			serviceMeshSpec.ControlPlane.Name = name
			serviceMeshSpec.ControlPlane.Namespace = namespace
		})

		It("should be able to remove mounted secret volumes on cleanup", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			createServiceMeshControlPlane(name, namespace)

			controlPlaneWithSecretVolumes, err := feature.CreateFeature("control-plane-with-secret-volumes").
				For(dsciSpec).
				Manifests(fromTestTmpDir(path.Join(feature.ControlPlaneDir, "base/control-plane-ingress.patch.tmpl"))).
				UsingConfig(envTest.Config).
				Load()

			Expect(err).ToNot(HaveOccurred())

			// when
			Expect(controlPlaneWithSecretVolumes.Apply()).To(Succeed())
			// Testing removal function on its own relying on feature setup
			Expect(servicemesh.RemoveTokenVolumes(controlPlaneWithSecretVolumes)).To(Succeed())

			// then
			serviceMeshControlPlane, err := getServiceMeshControlPlane(envTest.Config, namespace, name)
			Expect(err).ToNot(HaveOccurred())

			volumes, found, err := unstructured.NestedSlice(serviceMeshControlPlane.Object, "spec", "gateways", "ingress", "volumes")
			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(volumes).To(BeEmpty())
		})

		It("should be able to remove external provider on cleanup", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			serviceMeshSpec.Auth.Namespace = "test-provider"
			serviceMeshSpec.Auth.Authorino.Name = "authorino"

			createServiceMeshControlPlane(name, namespace)

			controlPlaneWithExtAuthzProvider, err := feature.CreateFeature("control-plane-with-external-authz-provider").
				For(dsciSpec).
				Manifests(fromTestTmpDir(path.Join(feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"))).
				UsingConfig(envTest.Config).
				Load()

			Expect(err).ToNot(HaveOccurred())

			// when
			By("verifying extension provider has been added after applying feature", func() {
				Expect(controlPlaneWithExtAuthzProvider.Apply()).To(Succeed())
				serviceMeshControlPlane, err := getServiceMeshControlPlane(envTest.Config, namespace, name)
				Expect(err).ToNot(HaveOccurred())

				extensionProviders, found, err := unstructured.NestedSlice(serviceMeshControlPlane.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue())

				extensionProvider := extensionProviders[0].(map[string]interface{})
				Expect(extensionProvider["name"]).To(Equal("test-odh-auth-provider"))
				Expect(extensionProvider["envoyExtAuthzGrpc"].(map[string]interface{})["service"]).To(Equal("authorino-authorino-authorization.test-provider.svc.cluster.local"))
			})

			// then
			By("verifying that extension provider has been removed", func() {
				err = servicemesh.RemoveExtensionProvider(controlPlaneWithExtAuthzProvider)
				serviceMeshControlPlane, err := getServiceMeshControlPlane(envTest.Config, namespace, name)
				Expect(err).ToNot(HaveOccurred())

				extensionProviders, found, err := unstructured.NestedSlice(serviceMeshControlPlane.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(extensionProviders).To(BeEmpty())
			})

		})

	})

})

func createServiceMeshControlPlane(name, namespace string) {
	serviceMeshControlPlane := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "maistra.io/v2",
			"kind":       "ServiceMeshControlPlane",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{},
		},
	}
	Expect(createSMCPInCluster(envTest.Config, serviceMeshControlPlane, namespace)).To(Succeed())
}

func createDataScienceProject(name string) *v1.Namespace {
	namespace := createNamespace(name)
	namespace.Labels = map[string]string{
		"opendatahub.io/dashboard": "true",
	}
	return namespace
}

func createNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func findMigratedNamespaces() []string {
	namespaces := &v1.NamespaceList{}
	var ns []string
	if err := envTestClient.List(context.Background(), namespaces); err != nil && !errors.IsNotFound(err) {
		Fail(err.Error())
	}
	for _, namespace := range namespaces.Items {
		if _, ok := namespace.ObjectMeta.Annotations["opendatahub.io/service-mesh"]; ok {
			ns = append(ns, namespace.Name)
		}
	}
	return ns
}

func newDSCInitializationSpec(ns string) *dscv1.DSCInitializationSpec {
	spec := dscv1.DSCInitializationSpec{}
	spec.ApplicationsNamespace = ns
	return &spec
}

// createSMCPInCluster uses dynamic client to create a dummy SMCP resource for testing
func createSMCPInCluster(cfg *rest.Config, smcpObj *unstructured.Unstructured, namespace string) error {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}

	result, err := dynamicClient.Resource(gvr.SMCP).Namespace(namespace).Create(context.TODO(), smcpObj, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	statusConditions := []interface{}{
		map[string]interface{}{
			"type":   "Ready",
			"status": "True",
		},
	}

	// Since we don't have actual service mesh operator deployed, we simulate the status
	status := map[string]interface{}{
		"conditions": statusConditions,
		"readiness": map[string]interface{}{
			"components": map[string]interface{}{
				"pending": []interface{}{},
				"ready": []interface{}{
					"istiod",
					"ingress-gateway",
				},
				"unready": []interface{}{},
			},
		},
	}

	if err := unstructured.SetNestedField(result.Object, status, "status"); err != nil {
		return err
	}

	_, err = dynamicClient.Resource(gvr.SMCP).Namespace(namespace).UpdateStatus(context.TODO(), result, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func getServiceMeshControlPlane(cfg *rest.Config, namespace, name string) (*unstructured.Unstructured, error) {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	smcp, err := dynamicClient.Resource(gvr.SMCP).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return smcp, nil
}

func getNamespace(namespace string) (*v1.Namespace, error) {
	ns := createNamespace(namespace)
	err := envTestClient.Get(context.Background(), types.NamespacedName{Name: namespace}, ns)

	return ns, err
}

func fromTestTmpDir(fileName string) string {
	root, err := envtestutil.FindProjectRoot()
	Expect(err).ToNot(HaveOccurred())

	tmpDir := filepath.Join(os.TempDir(), envtestutil.RandomUUIDName(16))
	if err := os.Mkdir(tmpDir, os.ModePerm); err != nil {
		Fail(err.Error())
	}

	src := path.Join(root, "pkg", "feature", fileName)
	dest := path.Join(tmpDir, fileName)
	if err := copyFile(src, dest); err != nil {
		Fail(err.Error())
	}

	return dest
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(dst), os.ModePerm); err != nil {
		return err
	}

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
