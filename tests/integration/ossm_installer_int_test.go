package ossm_test

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/ossm"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/ossm/feature"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig/ossmplugin"
	"github.com/opendatahub-io/opendatahub-operator/tests/integration/testenv"
	"io"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"os"
	"path"
	"path/filepath"
	"time"
)

const (
	timeout  = 5 * time.Second
	interval = 250 * time.Millisecond
)

var _ = Describe("preconditions", func() {

	Context("namespace existence", func() {

		var (
			objectCleaner *testenv.Cleaner
			testFeature   *feature.Feature
			namespace     string
		)

		BeforeEach(func() {
			objectCleaner = testenv.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

			testFeatureName := "test-ns-creation"
			namespace = testenv.GenerateNamespaceName(testFeatureName)

			ossmInstaller := newOssmInstaller(namespace)
			ossmPluginSpec, err := ossmInstaller.GetPluginSpec()
			Expect(err).ToNot(HaveOccurred())
			testFeature, err = feature.CreateFeature(testFeatureName).
				For(ossmPluginSpec).
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
			err = feature.CreateNamespace(namespace)(testFeature)

			// then
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not try to create namespace if it does already exist", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			// when
			err := feature.CreateNamespace(namespace)(testFeature)

			// then
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("ensuring custom resource definitions are installed", func() {

		var (
			ossmInstaller       *ossm.OssmInstaller
			ossmPluginSpec      *ossmplugin.OssmPluginSpec
			verificationFeature *feature.Feature
		)

		BeforeEach(func() {
			ossmInstaller = newOssmInstaller("default")
			var err error
			ossmPluginSpec, err = ossmInstaller.GetPluginSpec()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should successfully check for existing CRD", func() {
			// given example CRD installed into env from /ossm/test/crd/
			name := "test-resources.ossm.plugins.kubeflow.org"

			var err error
			verificationFeature, err = feature.CreateFeature("CRD verification").
				For(ossmPluginSpec).
				UsingConfig(envTest.Config).
				Preconditions(feature.EnsureCRDIsInstalled(name)).
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
				For(ossmPluginSpec).
				UsingConfig(envTest.Config).
				Preconditions(feature.EnsureCRDIsInstalled(name)).
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
		objectCleaner    *testenv.Cleaner
		ossmInstaller    *ossm.OssmInstaller
		ossmPluginSpec   *ossmplugin.OssmPluginSpec
		serviceMeshCheck *feature.Feature
		name             = "test-name"
		namespace        = "test-namespace"
	)

	BeforeEach(func() {
		ossmInstaller = newOssmInstaller(namespace)
		var err error
		ossmPluginSpec, err = ossmInstaller.GetPluginSpec()
		Expect(err).ToNot(HaveOccurred())

		ossmPluginSpec.Mesh.Name = name
		ossmPluginSpec.Mesh.Namespace = namespace

		serviceMeshCheck, err = feature.CreateFeature("datascience-project-migration").
			For(ossmPluginSpec).
			UsingConfig(envTest.Config).
			Preconditions(feature.EnsureServiceMeshInstalled).Load()

		Expect(err).ToNot(HaveOccurred())

		objectCleaner = testenv.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
	})

	It("should find installed SMCP", func() {
		ns := createNamespace(namespace)
		Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
		defer objectCleaner.DeleteAll(ns)

		createServiceMeshControlPlane(name, namespace)

		// when
		err := serviceMeshCheck.Apply()

		// then
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail to find SMCP if not present", func() {
		Expect(serviceMeshCheck.Apply()).To(HaveOccurred())
	})

})

var _ = Describe("Data Science Project Migration", func() {

	var (
		objectCleaner    *testenv.Cleaner
		ossmInstaller    *ossm.OssmInstaller
		ossmPluginSpec   *ossmplugin.OssmPluginSpec
		migrationFeature *feature.Feature
	)

	BeforeEach(func() {
		objectCleaner = testenv.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

		ossmInstaller = newOssmInstaller("default")

		var err error
		ossmPluginSpec, err = ossmInstaller.GetPluginSpec()
		Expect(err).ToNot(HaveOccurred())

		migrationFeature, err = feature.CreateFeature("datascience-project-migration").
			For(ossmPluginSpec).
			UsingConfig(envTest.Config).
			WithResources(feature.MigratedDataScienceProjects).Load()

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
		Expect(migrationFeature.Apply()).ToNot(HaveOccurred())

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
		Expect(migrationFeature.Apply()).ToNot(HaveOccurred())

		// then
		Consistently(findMigratedNamespaces, timeout, interval).Should(BeEmpty()) // we can't wait forever, but this should be good enough
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
		Expect(migrationFeature.Apply()).ToNot(HaveOccurred())

		// then
		Eventually(findMigratedNamespaces, timeout, interval).Should(
			And(
				HaveLen(3),
				ContainElements("dsp-01", "dsp-02", "dsp-03"),
			),
		)
	})

})

var _ = Describe("Feature enablement", func() {

	var (
		objectCleaner  *testenv.Cleaner
		ossmInstaller  *ossm.OssmInstaller
		ossmPluginSpec *ossmplugin.OssmPluginSpec
		name           = "test-name"
		namespace      = "test-namespace"
	)

	Context("installing Service Mesh Control Plane", func() {

		BeforeEach(func() {
			ossmInstaller = newOssmInstaller(namespace)
			var err error
			ossmPluginSpec, err = ossmInstaller.GetPluginSpec()
			Expect(err).ToNot(HaveOccurred())

			ossmPluginSpec.Mesh.Name = name
			ossmPluginSpec.Mesh.Namespace = namespace

			objectCleaner = testenv.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
		})

		It("should install control planed when enabled", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			ossmPluginSpec.Mesh.InstallationMode = ossmplugin.Minimal

			serviceMeshInstallation, err := feature.CreateFeature("control-plane-installation").
				For(ossmPluginSpec).
				UsingConfig(envTest.Config).
				Manifests(fromTestTmpDir(path.Join(feature.ControlPlaneDir, "control-plane-minimal.tmpl"))).
				EnabledIf(func(f *feature.Feature) bool {
					return f.Spec.Mesh.InstallationMode == ossmplugin.Minimal
				}).
				Load()

			Expect(err).ToNot(HaveOccurred())

			// when
			Expect(serviceMeshInstallation.Apply()).ToNot(HaveOccurred())

			// then
			controlPlane, err := getServiceMeshControlPlane(envTest.Config, namespace, name)
			Expect(err).ToNot(HaveOccurred())
			Expect(controlPlane.GetName()).To(Equal(name))
		})

		It("should not install control plane when disabled", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			serviceMeshInstallation, err := feature.CreateFeature("control-plane-installation").
				For(ossmPluginSpec).
				UsingConfig(envTest.Config).
				Manifests(fromTestTmpDir(path.Join(feature.ControlPlaneDir, "control-plane-minimal.tmpl"))).
				EnabledIf(func(f *feature.Feature) bool {
					return false
				}).
				Load()

			Expect(err).ToNot(HaveOccurred())

			// when
			Expect(serviceMeshInstallation.Apply()).ToNot(HaveOccurred())

			// then
			_, err = getServiceMeshControlPlane(envTest.Config, namespace, name)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should not install control plane by default", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			Expect(ossmPluginSpec.SetDefaults()).ToNot(HaveOccurred())

			serviceMeshInstallation, err := feature.CreateFeature("control-plane-installation").
				For(ossmPluginSpec).
				UsingConfig(envTest.Config).
				Manifests(fromTestTmpDir(path.Join(feature.ControlPlaneDir, "control-plane-minimal.tmpl"))).
				EnabledIf(func(f *feature.Feature) bool {
					return f.Spec.Mesh.InstallationMode != ossmplugin.PreInstalled
				}).
				Load()

			Expect(err).ToNot(HaveOccurred())

			// when
			Expect(serviceMeshInstallation.Apply()).ToNot(HaveOccurred())

			// then
			_, err = getServiceMeshControlPlane(envTest.Config, namespace, name)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

})

var _ = Describe("Cleanup operations", func() {

	Context("configuring control plane for auth(z)", func() {

		var (
			objectCleaner  *testenv.Cleaner
			ossmInstaller  *ossm.OssmInstaller
			ossmPluginSpec *ossmplugin.OssmPluginSpec
			namespace      = "test"
			name           = "minimal"
		)

		BeforeEach(func() {
			objectCleaner = testenv.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

			ossmInstaller = newOssmInstaller(namespace)

			var err error
			ossmPluginSpec, err = ossmInstaller.GetPluginSpec()
			Expect(err).ToNot(HaveOccurred())

			ossmPluginSpec.Mesh.Name = name
			ossmPluginSpec.Mesh.Namespace = namespace
		})

		It("should be able to remove mounted secret volumes on cleanup", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			createServiceMeshControlPlane(name, namespace)

			controlPlaneWithSecretVolumes, err := feature.CreateFeature("control-plane-with-secret-volumes").
				For(ossmPluginSpec).
				Manifests(fromTestTmpDir(path.Join(feature.ControlPlaneDir, "base/control-plane-ingress.patch.tmpl"))).
				UsingConfig(envTest.Config).
				Load()

			Expect(err).ToNot(HaveOccurred())

			// when
			Expect(controlPlaneWithSecretVolumes.Apply()).ToNot(HaveOccurred())
			// Testing removal function on its own relying on feature setup
			err = feature.RemoveTokenVolumes(controlPlaneWithSecretVolumes)

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

			ossmPluginSpec.Auth.Namespace = "test-provider"
			ossmPluginSpec.Auth.Authorino.Name = "authorino"

			createServiceMeshControlPlane(name, namespace)

			controlPlaneWithExtAuthzProvider, err := feature.CreateFeature("control-plane-with-external-authz-provider").
				For(ossmPluginSpec).
				Manifests(fromTestTmpDir(path.Join(feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl"))).
				UsingConfig(envTest.Config).
				Load()

			Expect(err).ToNot(HaveOccurred())

			// when
			By("verifying extension provider has been added after applying feature", func() {
				Expect(controlPlaneWithExtAuthzProvider.Apply()).ToNot(HaveOccurred())
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
				err = feature.RemoveExtensionProvider(controlPlaneWithExtAuthzProvider)
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
	createErr := createSMCPInCluster(envTest.Config, serviceMeshControlPlane, namespace)
	Expect(createErr).ToNot(HaveOccurred())
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

func newOssmInstaller(ns string) *ossm.OssmInstaller {
	config := kfconfig.KfConfig{}
	config.SetNamespace(ns)
	config.Spec.Plugins = append(config.Spec.Plugins, kfconfig.Plugin{
		Name: "KfOssmPlugin",
		Kind: "KfOssmPlugin",
	})
	return ossm.NewOssmInstaller(&config, envTest.Config)
}

// createSMCPInCluster uses dynamic client to create a dummy SMCP resource for testing
func createSMCPInCluster(cfg *rest.Config, smcpObj *unstructured.Unstructured, namespace string) error {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}

	gvr := schema.GroupVersionResource{
		Group:    "maistra.io",
		Version:  "v2",
		Resource: "servicemeshcontrolplanes",
	}

	result, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(context.TODO(), smcpObj, metav1.CreateOptions{})
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

	_, err = dynamicClient.Resource(gvr).Namespace(namespace).UpdateStatus(context.TODO(), result, metav1.UpdateOptions{})
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

	gvr := schema.GroupVersionResource{
		Group:    "maistra.io",
		Version:  "v2",
		Resource: "servicemeshcontrolplanes",
	}

	smcp, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
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
	root, err := findProjectRoot()
	Expect(err).ToNot(HaveOccurred())

	tmpDir := filepath.Join(os.TempDir(), testenv.RandomUUIDName(16))
	if err := os.Mkdir(tmpDir, os.ModePerm); err != nil {
		Fail(err.Error())
	}

	src := path.Join(root, "pkg/kfapp/ossm", fileName)
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
