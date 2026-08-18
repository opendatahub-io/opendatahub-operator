package datasciencepipelines_test

import (
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	dspgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	testmocks "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

func TestRegister_CleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newDataSciencePipelinesClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.DataSciencePipelinesComponentName, dspgate.Check)

	err = provision.GetUpgradeCheck(componentApi.DataSciencePipelinesComponentName)(
		ctx,
		cli,
		componentApi.DataSciencePipelinesComponentName,
		"",
	)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_StoredVersionRemovedPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newDataSciencePipelinesClient(newDSPACRD("v1"))
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.DataSciencePipelinesComponentName, dspgate.Check)

	err = provision.GetUpgradeCheck(componentApi.DataSciencePipelinesComponentName)(
		ctx,
		cli,
		componentApi.DataSciencePipelinesComponentName,
		"",
	)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_StoredVersionPresentBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newDataSciencePipelinesClient(newDSPACRD("v1alpha1", "v1"))
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.DataSciencePipelinesComponentName, dspgate.Check)

	err = provision.GetUpgradeCheck(componentApi.DataSciencePipelinesComponentName)(
		ctx,
		cli,
		componentApi.DataSciencePipelinesComponentName,
		"",
	)
	g.Expect(err).To(HaveOccurred())

	var blockingErr *dspgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.StoredVersion).To(Equal("v1alpha1"))
}

func newDataSciencePipelinesClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
	)
}

func newDSPACRD(storedVersions ...string) *apiextensionsv1.CustomResourceDefinition {
	crd := testmocks.NewMockCRD(
		"datasciencepipelinesapplications.opendatahub.io",
		"v1",
		"DataSciencePipelinesApplication",
		componentApi.DataSciencePipelinesComponentName,
	)
	crd.Name = "datasciencepipelinesapplications.datasciencepipelinesapplications.opendatahub.io"
	crd.Spec.Scope = apiextensionsv1.NamespaceScoped
	crd.Spec.Names.Singular = "datasciencepipelinesapplication"
	crd.Spec.Versions = make([]apiextensionsv1.CustomResourceDefinitionVersion, 0, len(storedVersions))
	for i, storedVersion := range storedVersions {
		crd.Spec.Versions = append(crd.Spec.Versions, apiextensionsv1.CustomResourceDefinitionVersion{
			Name:    storedVersion,
			Served:  true,
			Storage: i == 0,
			Schema: &apiextensionsv1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
				},
			},
		})
	}
	crd.Status.StoredVersions = storedVersions

	return crd
}
