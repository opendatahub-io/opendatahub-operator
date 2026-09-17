package v2

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	. "github.com/onsi/gomega"
	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

const (
	dscCRDName     = "datascienceclusters.datasciencecluster.opendatahub.io"
	dscCRDFilename = "datasciencecluster.opendatahub.io_datascienceclusters.yaml"
)

var retiredOperatorFields = []string{"trainingoperator", "llamastackoperator"}

func TestRetiredOperatorValidationEnvtest(t *testing.T) {
	g := NewWithT(t)
	projectDir, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())

	crdBytes, err := os.ReadFile(filepath.Join(projectDir, "config", "crd", "bases", dscCRDFilename))
	g.Expect(err).NotTo(HaveOccurred())
	var crd apiextensionsv1.CustomResourceDefinition
	g.Expect(yaml.Unmarshal(crdBytes, &crd)).To(Succeed())
	v2Version := crdVersion(t, &crd, "v2")
	validationRules := removeRetirementRules(t, v2Version.Schema.OpenAPIV3Schema)
	v2Version.Storage = true
	v2Version.Served = true
	crd.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{v2Version}
	crd.Spec.Conversion = nil

	testEnv := &envtest.Environment{
		CRDs: []*apiextensionsv1.CustomResourceDefinition{&crd},
	}
	cfg, err := testEnv.Start()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	})

	testScheme := runtime.NewScheme()
	g.Expect(AddToScheme(testScheme)).To(Succeed())
	g.Expect(apiextensionsv1.AddToScheme(testScheme)).To(Succeed())
	k8sClient, err := client.New(cfg, client.Options{Scheme: testScheme})
	g.Expect(err).NotTo(HaveOccurred())

	// Seed pre-existing Managed resources before the retirement CEL rules are
	// installed, matching upgrades where these values were already persisted.
	for _, field := range retiredOperatorFields {
		for _, scenario := range []string{"unchanged", "removed", "empty"} {
			name := field + "-" + scenario
			obj := retirementDSC(name, field, operatorv1.Managed)
			g.Expect(k8sClient.Create(t.Context(), obj)).To(Succeed())
		}
	}

	currentCRD := &apiextensionsv1.CustomResourceDefinition{}
	g.Expect(k8sClient.Get(t.Context(), client.ObjectKey{Name: dscCRDName}, currentCRD)).To(Succeed())
	currentV2 := crdVersion(t, currentCRD, "v2")
	setRetirementRules(currentV2.Schema.OpenAPIV3Schema, validationRules)
	updateCRDVersion(currentCRD, currentV2)
	g.Expect(k8sClient.Update(t.Context(), currentCRD)).To(Succeed())

	for _, field := range retiredOperatorFields {
		t.Run(field, func(t *testing.T) {
			g := NewWithT(t)

			newManaged := retirementDSC(field+"-new-managed", field, operatorv1.Managed)
			err := k8sClient.Create(t.Context(), newManaged)
			g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "creating a new Managed %s stanza must be rejected: %v", field, err)

			unchanged := &DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: field + "-unchanged"}}
			g.Expect(k8sClient.Get(t.Context(), client.ObjectKeyFromObject(unchanged), unchanged)).To(Succeed())
			unchanged.Labels = map[string]string{"retirement-test": "unrelated-update"}
			g.Expect(k8sClient.Update(t.Context(), unchanged)).To(Succeed(), "existing Managed %s must allow unrelated edits", field)

			for _, transition := range []struct {
				name  string
				state operatorv1.ManagementState
			}{
				{name: "removed", state: operatorv1.Removed},
				{name: "empty", state: ""},
			} {
				obj := &DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: field + "-" + transition.name}}
				g.Expect(k8sClient.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)).To(Succeed())
				setRetirementState(&obj.Spec.Components, field, transition.state)
				g.Expect(k8sClient.Update(t.Context(), obj)).To(Succeed(), "Managed to %q must be allowed for %s", transition.state, field)

				setRetirementState(&obj.Spec.Components, field, operatorv1.Managed)
				err := k8sClient.Update(t.Context(), obj)
				g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "%s must not be re-enabled from %q: %v", field, transition.state, err)
			}
		})
	}
}

func crdVersion(t *testing.T, crd *apiextensionsv1.CustomResourceDefinition, name string) apiextensionsv1.CustomResourceDefinitionVersion {
	t.Helper()
	for _, version := range crd.Spec.Versions {
		if version.Name == name {
			return version
		}
	}
	t.Fatalf("CRD %s has no %s version", crd.Name, name)
	return apiextensionsv1.CustomResourceDefinitionVersion{}
}

func removeRetirementRules(t *testing.T, schema *apiextensionsv1.JSONSchemaProps) map[string][]apiextensionsv1.ValidationRule {
	t.Helper()
	g := NewWithT(t)
	specSchema := schema.Properties["spec"]
	componentsSchema := specSchema.Properties["components"]
	rules := make(map[string][]apiextensionsv1.ValidationRule, len(retiredOperatorFields))
	for _, field := range retiredOperatorFields {
		fieldSchema := componentsSchema.Properties[field]
		rules[field] = slices.Clone(fieldSchema.XValidations)
		g.Expect(rules[field]).NotTo(BeEmpty(), "expected generated retirement CEL rule for %s", field)
		fieldSchema.XValidations = nil
		componentsSchema.Properties[field] = fieldSchema
	}
	specSchema.Properties["components"] = componentsSchema
	schema.Properties["spec"] = specSchema
	return rules
}

func setRetirementRules(schema *apiextensionsv1.JSONSchemaProps, rules map[string][]apiextensionsv1.ValidationRule) {
	specSchema := schema.Properties["spec"]
	componentsSchema := specSchema.Properties["components"]
	for _, field := range retiredOperatorFields {
		fieldSchema := componentsSchema.Properties[field]
		fieldSchema.XValidations = slices.Clone(rules[field])
		componentsSchema.Properties[field] = fieldSchema
	}
	specSchema.Properties["components"] = componentsSchema
	schema.Properties["spec"] = specSchema
}

func updateCRDVersion(crd *apiextensionsv1.CustomResourceDefinition, replacement apiextensionsv1.CustomResourceDefinitionVersion) {
	for i := range crd.Spec.Versions {
		if crd.Spec.Versions[i].Name == replacement.Name {
			crd.Spec.Versions[i] = replacement
			return
		}
	}
}

func retirementDSC(name, field string, state operatorv1.ManagementState) *DataScienceCluster {
	dsc := &DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	setRetirementState(&dsc.Spec.Components, field, state)
	return dsc
}

func setRetirementState(components *Components, field string, state operatorv1.ManagementState) {
	switch field {
	case "trainingoperator":
		components.TrainingOperator.ManagementState = state
	case "llamastackoperator":
		components.LlamaStackOperator.ManagementState = state
	default:
		panic("unknown retired operator field: " + field)
	}
}
