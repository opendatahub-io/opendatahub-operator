//nolint:testpackage
package trainer

import (
	"fmt"
	"testing"

	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	testf "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const jobSetOperatorRndVersion = "1.1.0"

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

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	jobSetOperatorListGVK := schema.GroupVersionKind{
		Group:   gvk.JobSetOperatorV1.Group,
		Version: gvk.JobSetOperatorV1.Version,
		Kind:    "JobSetOperatorList",
	}
	fakeSchema.AddKnownTypeWithName(gvk.JobSetOperatorV1, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(jobSetOperatorListGVK, &unstructured.UnstructuredList{})

	jobSetOperatorCondition := &ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
		Name: fmt.Sprintf("%s.%s", jobSetOperator, jobSetOperatorRndVersion),
	}}

	jobSetOperatorCR := &unstructured.Unstructured{}
	jobSetOperatorCR.SetGroupVersionKind(gvk.JobSetOperatorV1)
	jobSetOperatorCR.SetName("cluster")
	err = testf.SetTypedConditions(jobSetOperatorCR, []metav1.Condition{
		{
			Type:   "Available",
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(
		fakeclient.WithScheme(fakeSchema),
		fakeclient.WithObjects(jobSetOperatorCondition, jobSetOperatorCR),
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

func TestCheckPreConditions_JobSetOperatorCRNotFound(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	jobSetOperatorListGVK := schema.GroupVersionKind{
		Group:   gvk.JobSetOperatorV1.Group,
		Version: gvk.JobSetOperatorV1.Version,
		Kind:    "JobSetOperatorList",
	}
	fakeSchema.AddKnownTypeWithName(gvk.JobSetv1alpha2, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(gvk.JobSetOperatorV1, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(jobSetOperatorListGVK, &unstructured.UnstructuredList{})

	jobSetOperatorCondition := &ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
		Name: fmt.Sprintf("%s.%s", jobSetOperator, jobSetOperatorRndVersion),
	}}

	cli, err := fakeclient.New(
		fakeclient.WithScheme(fakeSchema),
		fakeclient.WithObjects(jobSetOperatorCondition),
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
	g.Expect(err).To(MatchError(ContainSubstring(status.JobSetOperatorCRNotFoundMessage)))
}

func TestCheckPreConditions_JobSetOperatorCRWrongName(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	jobSetOperatorListGVK := schema.GroupVersionKind{
		Group:   gvk.JobSetOperatorV1.Group,
		Version: gvk.JobSetOperatorV1.Version,
		Kind:    "JobSetOperatorList",
	}
	fakeSchema.AddKnownTypeWithName(gvk.JobSetv1alpha2, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(gvk.JobSetOperatorV1, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(jobSetOperatorListGVK, &unstructured.UnstructuredList{})

	jobSetOperatorCondition := &ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
		Name: fmt.Sprintf("%s.%s", jobSetOperator, jobSetOperatorRndVersion),
	}}

	wrongNameCR := &unstructured.Unstructured{}
	wrongNameCR.SetGroupVersionKind(gvk.JobSetOperatorV1)
	wrongNameCR.SetName("wrong-name")

	cli, err := fakeclient.New(
		fakeclient.WithScheme(fakeSchema),
		fakeclient.WithObjects(jobSetOperatorCondition, wrongNameCR),
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
	expectedMessage := fmt.Sprintf(status.JobSetOperatorCRWrongNameMessage, "wrong-name")
	g.Expect(err).To(MatchError(ContainSubstring(expectedMessage)))
	g.Expect(err.Error()).To(ContainSubstring("wrong-name"))
}

func TestCheckPreConditions_JobSetOperatorCRWrongNameWithClusterCR(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	jobSetOperatorListGVK := schema.GroupVersionKind{
		Group:   gvk.JobSetOperatorV1.Group,
		Version: gvk.JobSetOperatorV1.Version,
		Kind:    "JobSetOperatorList",
	}
	fakeSchema.AddKnownTypeWithName(gvk.JobSetv1alpha2, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(gvk.JobSetOperatorV1, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(jobSetOperatorListGVK, &unstructured.UnstructuredList{})

	jobSetOperatorCondition := &ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
		Name: fmt.Sprintf("%s.%s", jobSetOperator, jobSetOperatorRndVersion),
	}}

	clusterCR := &unstructured.Unstructured{}
	clusterCR.SetGroupVersionKind(gvk.JobSetOperatorV1)
	clusterCR.SetName("cluster")
	err = testf.SetTypedConditions(clusterCR, []metav1.Condition{
		{
			Type:   "Available",
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	wrongNameCR := &unstructured.Unstructured{}
	wrongNameCR.SetGroupVersionKind(gvk.JobSetOperatorV1)
	wrongNameCR.SetName("another-wrong-name")

	cli, err := fakeclient.New(
		fakeclient.WithScheme(fakeSchema),
		fakeclient.WithObjects(jobSetOperatorCondition, clusterCR, wrongNameCR),
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
	expectedMessage := fmt.Sprintf(status.JobSetOperatorCRWrongNameMessage, "another-wrong-name")
	g.Expect(err).To(MatchError(ContainSubstring(expectedMessage)))
	g.Expect(err.Error()).To(ContainSubstring("another-wrong-name"))
}

func TestCheckPreConditions_Success(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeSchema, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	jobSetOperatorListGVK := schema.GroupVersionKind{
		Group:   gvk.JobSetOperatorV1.Group,
		Version: gvk.JobSetOperatorV1.Version,
		Kind:    "JobSetOperatorList",
	}
	fakeSchema.AddKnownTypeWithName(gvk.JobSetv1alpha2, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(gvk.JobSetOperatorV1, &unstructured.Unstructured{})
	fakeSchema.AddKnownTypeWithName(jobSetOperatorListGVK, &unstructured.UnstructuredList{})

	jobSetCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "jobsets.jobset.x-k8s.io",
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			StoredVersions: []string{gvk.JobSetv1alpha2.Version},
		},
	}
	jobSetOperatorCondition := &ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
		Name: fmt.Sprintf("%s.%s", jobSetOperator, jobSetOperatorRndVersion),
	}}

	jobSetOperatorCR := &unstructured.Unstructured{}
	jobSetOperatorCR.SetGroupVersionKind(gvk.JobSetOperatorV1)
	jobSetOperatorCR.SetName("cluster")
	err = testf.SetTypedConditions(jobSetOperatorCR, []metav1.Condition{
		{
			Type:   "Available",
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		},
	})
	g.Expect(err).ShouldNot(HaveOccurred())

	cli, err := fakeclient.New(
		fakeclient.WithScheme(fakeSchema),
		fakeclient.WithObjects(jobSetCRD, jobSetOperatorCondition, jobSetOperatorCR),
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

func TestJobSetConditionFilter(t *testing.T) {
	tests := []struct {
		name           string
		conditionType  string
		conditionValue string
		shouldDegrade  bool
	}{
		// Degraded conditions
		{
			name:           "Degraded=True triggers degradation",
			conditionType:  "Degraded",
			conditionValue: "True",
			shouldDegrade:  true,
		},
		{
			name:           "TargetConfigControllerDegraded=True triggers degradation",
			conditionType:  "TargetConfigControllerDegraded",
			conditionValue: "True",
			shouldDegrade:  true,
		},
		{
			name:           "JobSetOperatorStaticResourcesDegraded=True triggers degradation",
			conditionType:  "JobSetOperatorStaticResourcesDegraded",
			conditionValue: "True",
			shouldDegrade:  true,
		},
		// Healthy conditions
		{
			name:           "Degraded=False is healthy",
			conditionType:  "Degraded",
			conditionValue: "False",
			shouldDegrade:  false,
		},
		{
			name:           "TargetConfigControllerDegraded=False is healthy",
			conditionType:  "TargetConfigControllerDegraded",
			conditionValue: "False",
			shouldDegrade:  false,
		},
		{
			name:           "JobSetOperatorStaticResourcesDegraded=False is healthy",
			conditionType:  "JobSetOperatorStaticResourcesDegraded",
			conditionValue: "False",
			shouldDegrade:  false,
		},
		{
			name:           "Available=False triggers degradation",
			conditionType:  "Available",
			conditionValue: "False",
			shouldDegrade:  true,
		},
		{
			name:           "Available=True is healthy",
			conditionType:  "Available",
			conditionValue: "True",
			shouldDegrade:  false,
		},
		// Conditions not in filter (should be ignored)
		{
			name:           "Unknown condition type is ignored",
			conditionType:  "SomeOtherCondition",
			conditionValue: "True",
			shouldDegrade:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result := jobSetConditionFilter(tt.conditionType, tt.conditionValue)
			g.Expect(result).To(Equal(tt.shouldDegrade))
		})
	}
}
