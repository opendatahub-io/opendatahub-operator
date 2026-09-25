package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

func TestDashboardV3ManagementStateSpecPath(t *testing.T) {
	g := NewWithT(t)
	path := componentManagementStateSpecPath(componentApi.DashboardKind, "dashboard")
	g.Expect(path).To(Equal(".spec.components.dashboard.standard.managementState"))
	g.Expect(componentManagementStateSpecPath("Workbenches", "workbenches")).To(Equal(".spec.components.workbenches.managementState"))

	obj := &unstructured.Unstructured{Object: map[string]any{}}
	g.Expect(testf.Transform(`%s = "Managed"`, path)(obj)).To(Succeed())
	state, found, err := unstructured.NestedString(obj.Object, "spec", "components", "dashboard", "standard", "managementState")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(state).To(Equal("Managed"))
	_, found, err = unstructured.NestedFieldNoCopy(obj.Object, "spec", "components", "dashboard", "managementState")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeFalse())

	root, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())
	data, err := os.ReadFile(filepath.Join(root, "config", "crd", "bases", "datasciencecluster.opendatahub.io_datascienceclusters.yaml"))
	g.Expect(err).NotTo(HaveOccurred())
	var crd apiextensionsv1.CustomResourceDefinition
	g.Expect(yaml.Unmarshal(data, &crd)).To(Succeed())
	for _, version := range crd.Spec.Versions {
		if version.Name != "v3" {
			continue
		}
		spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
		components := spec.Properties["components"]
		dashboard := components.Properties["dashboard"]
		g.Expect(dashboard.Properties).NotTo(HaveKey("managementState"))
		g.Expect(dashboard.Properties["standard"].Properties).To(HaveKey("managementState"))
		return
	}
	t.Fatal("DSC CRD has no v3 schema")
}
