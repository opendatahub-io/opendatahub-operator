package workbenches_test

import (
	"embed"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	metadataannotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	workbenchesgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/workbenches"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

//go:embed resources
var resourcesFS embed.FS

const (
	testNamespace         = "user-ns"
	testApplicationNS     = "redhat-ods-applications"
	hwpNameAnnotation     = "opendatahub.io/hardware-profile-name"
	hwpNamespaceAnnotion  = "opendatahub.io/hardware-profile-namespace"
	acceleratorAnnotation = "opendatahub.io/accelerator-name"
	lastSizeAnnotation    = "notebooks.opendatahub.io/last-size-selection"
)

func TestRegister_CleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newWorkbenchesClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_NotebookWithoutHardwareProfileAnnotationPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "plain-notebook",
		"Namespace": testNamespace,
	})

	cli, err := newWorkbenchesClient(notebook)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_NotebookWithExistingHardwareProfilePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "migrated-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			hwpNameAnnotation:    "gpu-small-notebooks",
			hwpNamespaceAnnotion: testNamespace,
		},
	})
	hwp := renderHardwareProfile(t, map[string]any{
		"Name":      "gpu-small-notebooks",
		"Namespace": testNamespace,
	})

	cli, err := newWorkbenchesClient(notebook, hwp)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_NotebookWithApplicationNamespaceHardwareProfilePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "shared-hwp-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			hwpNameAnnotation:    "shared-notebooks",
			hwpNamespaceAnnotion: testApplicationNS,
		},
	})
	hwp := renderHardwareProfile(t, map[string]any{
		"Name":      "shared-notebooks",
		"Namespace": testApplicationNS,
	})

	cli, err := newWorkbenchesClient(notebook, hwp)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_NotebookWithExistingConnectionsPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "connected-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			metadataannotations.Connection: "data-conn,other-ns/shared-conn",
		},
	})
	localSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "data-conn", Namespace: testNamespace}}
	sharedSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shared-conn", Namespace: "other-ns"}}

	cli, err := newWorkbenchesClient(notebook, localSecret, sharedSecret)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_NotebookWithMissingHardwareProfileBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "broken-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			hwpNameAnnotation:    "missing-hwp",
			hwpNamespaceAnnotion: testNamespace,
		},
	})

	cli, err := newWorkbenchesClient(notebook)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *workbenchesgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.NotebooksWithBrokenHardwareProfileRefs).To(Equal(1))
	g.Expect(blockingErr.NotebooksWithBrokenConnectionRefs).To(Equal(0))
}

func TestRegister_NotebookWithMissingConnectionBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "broken-connection-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			metadataannotations.Connection: "missing-secret",
		},
	})

	cli, err := newWorkbenchesClient(notebook)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *workbenchesgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.NotebooksWithBrokenHardwareProfileRefs).To(Equal(0))
	g.Expect(blockingErr.NotebooksWithBrokenConnectionRefs).To(Equal(1))
	g.Expect(blockingErr.NotebooksWithContainerNameMismatch).To(Equal(0))
}

func TestRegister_DashboardManagedNotebookWithMatchingContainerNamePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "dashboard-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			lastSizeAnnotation: "Small",
		},
	})
	setNotebookContainers(t, notebook,
		container("dashboard-notebook", "jupyter:latest"),
		container("oauth-proxy", "registry/ose-oauth-proxy-rhel9:latest"),
	)

	cli, err := newWorkbenchesClient(notebook)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_DashboardManagedNotebookWithMismatchedContainerNameBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "dashboard-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			acceleratorAnnotation: "gpu-profile",
		},
	})
	setNotebookContainers(t, notebook, container("legacy-name", "jupyter:latest"))

	cli, err := newWorkbenchesClient(notebook)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *workbenchesgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.NotebooksWithBrokenHardwareProfileRefs).To(Equal(0))
	g.Expect(blockingErr.NotebooksWithBrokenConnectionRefs).To(Equal(0))
	g.Expect(blockingErr.NotebooksWithContainerNameMismatch).To(Equal(1))
}

func TestRegister_NotebookWithMultipleWorkloadContainersDoesNotBlockOnNameMismatch(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	notebook := renderNotebook(t, map[string]any{
		"Name":      "dashboard-notebook",
		"Namespace": testNamespace,
		"Annotations": map[string]string{
			acceleratorAnnotation: "gpu-profile",
		},
	})
	setNotebookContainers(t, notebook,
		container("legacy-name", "jupyter:latest"),
		container("helper", "busybox:latest"),
	)

	cli, err := newWorkbenchesClient(notebook)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.WorkbenchesComponentName, workbenchesgate.Check)

	err = provision.GetUpgradeCheck(componentApi.WorkbenchesComponentName)(ctx, cli, componentApi.WorkbenchesComponentName, testApplicationNS)
	g.Expect(err).ToNot(HaveOccurred())
}

func newWorkbenchesClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.Notebook, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.HardwareProfile, Scope: meta.RESTScopeNamespace},
		),
	)
}

func renderNotebook(t *testing.T, data map[string]any) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/notebook.tmpl.yaml", data)
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}

func setNotebookContainers(t *testing.T, notebook *unstructured.Unstructured, containers ...map[string]any) {
	t.Helper()

	g := NewWithT(t)
	raw := make([]any, 0, len(containers))
	for _, ctr := range containers {
		raw = append(raw, ctr)
	}

	err := unstructured.SetNestedSlice(notebook.Object, raw, "spec", "template", "spec", "containers")
	g.Expect(err).ToNot(HaveOccurred())
}

func container(name string, image string) map[string]any {
	return map[string]any{
		"name":  name,
		"image": image,
	}
}

func renderHardwareProfile(t *testing.T, data map[string]any) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/hardwareprofile.tmpl.yaml", data)
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}
