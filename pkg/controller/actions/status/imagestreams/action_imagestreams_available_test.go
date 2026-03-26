package imagestreams_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/onsi/gomega/gstruct"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/imagestreams"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega"
)

func newDSCI(ns string) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsci"},
		Spec:       dsciv2.DSCInitializationSpec{ApplicationsNamespace: ns},
	}
}

func newImageStream(name, ns string, partOf string, tagStatuses []imagev1.NamedTagEventList) *imagev1.ImageStream {
	return &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				labels.PlatformPartOf: partOf,
			},
		},
		Status: imagev1.ImageStreamStatus{
			Tags: tagStatuses,
		},
	}
}

func TestImageStreamsNoImageStreams(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns)))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
}

func TestImageStreamsAllHealthy(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	is := newImageStream("jupyter-datascience", ns, strings.ToLower(componentApi.WorkbenchesKind), []imagev1.NamedTagEventList{
		{
			Tag:   "latest",
			Items: []imagev1.TagEvent{{Image: "sha256:abc123"}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
}

func TestImageStreamsAllFailed(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	is := newImageStream("jupyter-cuda", ns, strings.ToLower(componentApi.WorkbenchesKind), []imagev1.NamedTagEventList{
		{
			Tag:   "cuda-12",
			Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type:    imagev1.ImportSuccess,
				Status:  corev1.ConditionFalse,
				Message: "image not found",
			}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status":  Equal(metav1.ConditionFalse),
				"Reason":  Equal(status.ConditionImageStreamsNotAvailableReason),
				"Message": ContainSubstring("Warning:"),
			}),
		),
	)
}

func TestImageStreamsMixedHealth(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	is := newImageStream("jupyter-mixed", ns, strings.ToLower(componentApi.WorkbenchesKind), []imagev1.NamedTagEventList{
		{
			Tag:   "cpu",
			Items: []imagev1.TagEvent{{Image: "sha256:abc"}},
		},
		{
			Tag:   "cuda",
			Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type:    imagev1.ImportSuccess,
				Status:  corev1.ConditionFalse,
				Message: "not found",
			}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status":  Equal(metav1.ConditionFalse),
				"Message": And(ContainSubstring("1 ImageStream tag(s)"), ContainSubstring("jupyter-mixed:cuda")),
			}),
		),
	)
}

func TestImageStreamsFreshDeploy(t *testing.T) {
	// Fresh deploy: no items, no conditions — should not be marked as failed
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	is := newImageStream("jupyter-new", ns, strings.ToLower(componentApi.WorkbenchesKind), []imagev1.NamedTagEventList{
		{
			Tag:   "latest",
			Items: []imagev1.TagEvent{},
			// No conditions — import hasn't been attempted yet
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
}

func TestImageStreamsImportSuccessTrue(t *testing.T) {
	// ImportSuccess=True with no items yet: should still count as healthy
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	is := newImageStream("jupyter-importing", ns, strings.ToLower(componentApi.WorkbenchesKind), []imagev1.NamedTagEventList{
		{
			Tag:   "latest",
			Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type:   imagev1.ImportSuccess,
				Status: corev1.ConditionTrue,
			}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
}

func TestImageStreamsMultipleImageStreams(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	partOf := strings.ToLower(componentApi.WorkbenchesKind)
	is1 := newImageStream("jupyter-cpu", ns, partOf, []imagev1.NamedTagEventList{
		{Tag: "latest", Items: []imagev1.TagEvent{{Image: "sha256:ok"}}},
	})
	is2 := newImageStream("jupyter-cuda", ns, partOf, []imagev1.NamedTagEventList{
		{
			Tag: "cuda-12", Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type: imagev1.ImportSuccess, Status: corev1.ConditionFalse, Message: "not found",
			}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is1, is2))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(imagestreams.WithSelectorLabel(labels.PlatformPartOf, partOf))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status":  Equal(metav1.ConditionFalse),
				"Message": ContainSubstring("jupyter-cuda:cuda-12"),
			}),
		),
	)
}

func TestImageStreamsIgnoresDifferentLabels(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	// ImageStream with a different partOf label — should be ignored
	is := newImageStream("other-component", ns, "dashboard", []imagev1.NamedTagEventList{
		{
			Tag: "bad", Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type: imagev1.ImportSuccess, Status: corev1.ConditionFalse, Message: "fail",
			}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionTrue),
			}),
		),
	)
}

func TestImageStreamsInNamespace(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()
	otherNs := xid.New().String()

	partOf := strings.ToLower(componentApi.WorkbenchesKind)
	// ImageStream in the target namespace — should be detected
	isTarget := newImageStream("jupyter-target", ns, partOf, []imagev1.NamedTagEventList{
		{
			Tag: "bad", Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type: imagev1.ImportSuccess, Status: corev1.ConditionFalse, Message: "fail",
			}},
		},
	})
	// ImageStream in a different namespace — should be ignored
	isOther := newImageStream("jupyter-other", otherNs, partOf, []imagev1.NamedTagEventList{
		{
			Tag: "bad", Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type: imagev1.ImportSuccess, Status: corev1.ConditionFalse, Message: "fail",
			}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), isTarget, isOther))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.InNamespace(ns),
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, partOf))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status":  Equal(metav1.ConditionFalse),
				"Message": And(ContainSubstring("jupyter-target:bad"), Not(ContainSubstring("jupyter-other"))),
			}),
		),
	)
}

func TestImageStreamsMessageTruncation(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	longMsg := strings.Repeat("x", 200)
	is := newImageStream("jupyter-long", ns, strings.ToLower(componentApi.WorkbenchesKind), []imagev1.NamedTagEventList{
		{
			Tag: "tag1", Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type: imagev1.ImportSuccess, Status: corev1.ConditionFalse, Message: longMsg,
			}},
		},
	})

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status":  Equal(metav1.ConditionFalse),
				"Message": And(ContainSubstring("..."), Not(ContainSubstring(longMsg))),
			}),
		),
	)
}

func TestImageStreamsMaxFailedTagsCap(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	ns := xid.New().String()

	// Create an ImageStream with 15 failed tags — more than the maxFailedTags cap (10).
	tags := make([]imagev1.NamedTagEventList, 0, 15)
	for i := range 15 {
		tags = append(tags, imagev1.NamedTagEventList{
			Tag: fmt.Sprintf("tag-%d", i), Items: []imagev1.TagEvent{},
			Conditions: []imagev1.TagEventCondition{{
				Type: imagev1.ImportSuccess, Status: corev1.ConditionFalse, Message: "not found",
			}},
		})
	}

	is := newImageStream("jupyter-many-tags", ns, strings.ToLower(componentApi.WorkbenchesKind), tags)

	cl, err := fakeclient.New(fakeclient.WithObjects(newDSCI(ns), is))
	g.Expect(err).ShouldNot(HaveOccurred())

	action := imagestreams.NewAction(
		imagestreams.WithSelectorLabel(labels.PlatformPartOf, strings.ToLower(componentApi.WorkbenchesKind)))

	rr := types.ReconciliationRequest{
		Client:   cl,
		Instance: &componentApi.Workbenches{},
		Release:  common.Release{Name: cluster.OpenDataHub},
	}
	rr.Conditions = conditions.NewManager(rr.Instance, status.ConditionTypeReady)

	err = action(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rr.Instance).Should(
		WithTransform(
			matchers.ExtractStatusCondition(status.ConditionImageStreamsAvailable),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(metav1.ConditionFalse),
				"Message": And(
					ContainSubstring("Warning: 15 ImageStream tag(s) failed to import"),
					ContainSubstring("... and 5 more"),
					Not(ContainSubstring("tag-14")),
				),
			}),
		),
	)
}
