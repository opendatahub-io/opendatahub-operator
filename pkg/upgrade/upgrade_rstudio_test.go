package upgrade_test

import (
	"testing"

	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const testAppNamespace = "test-app-ns"

func newTestDSCI(appNamespace string) *unstructured.Unstructured {
	dsci := &unstructured.Unstructured{}
	dsci.SetGroupVersionKind(gvk.DSCInitialization)
	dsci.SetName("test-dsci")
	dsci.SetNamespace("test-namespace")
	_ = unstructured.SetNestedField(dsci.Object, appNamespace, "spec", "applicationsNamespace")
	return dsci
}

func newTestRStudioBuildConfig(name, namespace string) *buildv1.BuildConfig {
	return &buildv1.BuildConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func newTestRStudioNotebookImageStream(name string, tags ...string) *imagev1.ImageStream {
	is := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testAppNamespace,
		},
		Spec: imagev1.ImageStreamSpec{
			Tags: make([]imagev1.TagReference, 0, len(tags)),
		},
	}

	for _, tagName := range tags {
		is.Spec.Tags = append(is.Spec.Tags, imagev1.TagReference{
			Name: tagName,
			Annotations: map[string]string{
				"opendatahub.io/workbench-image-recommended": "true",
			},
		})
	}

	return is
}

func newUpgradeTestScheme(g *WithT) *runtime.Scheme {
	s, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(buildv1.Install(s)).Should(Succeed())
	return s
}

func TestCleanupDeprecatedRStudioResources(t *testing.T) {
	ctx := t.Context()

	t.Run("should delete BuildConfigs and build ImageStreams and deprecate notebook ImageStreams", func(t *testing.T) {
		g := NewWithT(t)

		objects := []client.Object{
			newTestDSCI(testAppNamespace),
			newTestRStudioBuildConfig("rstudio-server-rhel9", testAppNamespace),
			newTestRStudioBuildConfig("cuda-rstudio-server-rhel9", testAppNamespace),
			newTestRStudioNotebookImageStream("rstudio-rhel9", "latest"),
			newTestRStudioNotebookImageStream("cuda-rstudio-rhel9", "latest"),
			newTestRStudioNotebookImageStream("rstudio-notebook", "3.4", "2025.2"),
			newTestRStudioNotebookImageStream("rstudio-gpu-notebook", "3.4", "2025.2"),
		}

		cli, err := fakeclient.New(
			fakeclient.WithScheme(newUpgradeTestScheme(g)),
			fakeclient.WithObjects(objects...),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli, "")
		g.Expect(err).ShouldNot(HaveOccurred())

		for _, name := range []string{"rstudio-server-rhel9", "cuda-rstudio-server-rhel9"} {
			bc := &buildv1.BuildConfig{}
			err = cli.Get(ctx, client.ObjectKey{Name: name, Namespace: testAppNamespace}, bc)
			g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
		}

		for _, name := range []string{"rstudio-rhel9", "cuda-rstudio-rhel9"} {
			is := &imagev1.ImageStream{}
			err = cli.Get(ctx, client.ObjectKey{Name: name, Namespace: testAppNamespace}, is)
			g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
		}

		for _, name := range []string{"rstudio-notebook", "rstudio-gpu-notebook"} {
			is := &imagev1.ImageStream{}
			err = cli.Get(ctx, client.ObjectKey{Name: name, Namespace: testAppNamespace}, is)
			g.Expect(err).ShouldNot(HaveOccurred())
			for _, tag := range is.Spec.Tags {
				g.Expect(tag.Annotations).To(HaveKeyWithValue("opendatahub.io/image-tag-outdated", "true"))
				g.Expect(tag.Annotations).To(HaveKeyWithValue("opendatahub.io/workbench-image-recommended", "false"))
			}
		}
	})

	t.Run("should be idempotent when RStudio resources are already cleaned up", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New(
			fakeclient.WithScheme(newUpgradeTestScheme(g)),
			fakeclient.WithObjects(newTestDSCI(testAppNamespace)),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		for range 2 {
			err = upgrade.CleanupExistingResource(ctx, cli, "")
			g.Expect(err).ShouldNot(HaveOccurred())
		}
	})

	t.Run("should be idempotent when notebook ImageStreams are already deprecated", func(t *testing.T) {
		g := NewWithT(t)

		is := newTestRStudioNotebookImageStream("rstudio-gpu-notebook", "3.4")
		for i := range is.Spec.Tags {
			is.Spec.Tags[i].Annotations["opendatahub.io/image-tag-outdated"] = "true"
			is.Spec.Tags[i].Annotations["opendatahub.io/workbench-image-recommended"] = "false"
		}

		cli, err := fakeclient.New(
			fakeclient.WithScheme(newUpgradeTestScheme(g)),
			fakeclient.WithObjects(newTestDSCI(testAppNamespace), is),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		for range 2 {
			err = upgrade.CleanupExistingResource(ctx, cli, "")
			g.Expect(err).ShouldNot(HaveOccurred())
		}

		updated := &imagev1.ImageStream{}
		err = cli.Get(ctx, client.ObjectKey{Name: "rstudio-gpu-notebook", Namespace: testAppNamespace}, updated)
		g.Expect(err).ShouldNot(HaveOccurred())
		for _, tag := range updated.Spec.Tags {
			g.Expect(tag.Annotations).To(HaveKeyWithValue("opendatahub.io/image-tag-outdated", "true"))
			g.Expect(tag.Annotations).To(HaveKeyWithValue("opendatahub.io/workbench-image-recommended", "false"))
		}
	})
}
