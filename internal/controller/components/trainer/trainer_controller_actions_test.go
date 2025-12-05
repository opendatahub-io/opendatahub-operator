//nolint:testpackage
package trainer

import (
	"testing"

	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions_Managed_JobSetOperatorNotInstalled(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	trainer := componentApi.Trainer{
		Spec: componentApi.TrainerSpec{},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &trainer,
		Conditions: conditions.NewManager(&trainer, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(status.JobSetOperatorNotInstalledMessage)))
}

func TestCheckPreConditions_Managed_JobSetCRDNotInstalled(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: jobSetOperator,
			}},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	trainer := componentApi.Trainer{
		Spec: componentApi.TrainerSpec{},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &trainer,
		Conditions: conditions.NewManager(&trainer, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring(status.JobSetCRDMissingMessage)))
}

func TestCheckPreConditions_Managed_JobSetCRDInstalled(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	fakeSchema.AddKnownTypeWithName(gvk.JobSetv1alpha2, &unstructured.Unstructured{})

	jobSetCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "jobsets.jobset.x-k8s.io",
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			StoredVersions: []string{gvk.JobSetv1alpha2.Version},
		},
	}
	jobSetOperatorCondition := &ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
		Name: jobSetOperator,
	}}

	cli, err := fakeclient.New(
		fakeclient.WithScheme(fakeSchema),
		fakeclient.WithObjects(jobSetCRD, jobSetOperatorCondition),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	trainer := componentApi.Trainer{
		Spec: componentApi.TrainerSpec{},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &trainer,
		Conditions: conditions.NewManager(&trainer, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}
