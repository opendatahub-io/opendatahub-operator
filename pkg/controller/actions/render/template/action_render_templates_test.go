package template_test

import (
	"context"
	"embed"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apytypes "k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

//go:embed resources
var testFS embed.FS

func TestRenderTemplate(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := template.NewAction()

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		},
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: ns,
				ServiceMesh: &infrav1.ServiceMeshSpec{
					ControlPlane: infrav1.ControlPlaneSpec{
						Name:      xid.New().String(),
						Namespace: xid.New().String(),
					},
				},
			},
		},
		Release:   common.Release{Name: cluster.OpenDataHub},
		Templates: []types.TemplateInfo{{FS: testFS, Path: "resources/smm.tmpl.yaml"}},
	}

	err = action(ctx, &rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.namespace == "%s"`, ns),
			jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, rr.DSCI.Spec.ServiceMesh.ControlPlane.Namespace),
			jq.Match(`.spec.controlPlaneRef.name == "%s"`, rr.DSCI.Spec.ServiceMesh.ControlPlane.Name),
			jq.Match(`.metadata.annotations."instance-name" == "%s"`, rr.Instance.GetName()),
		)),
	))
}

func TestRenderTemplateWithData(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()
	id := xid.New().String()
	name := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := template.NewAction(
		template.WithData(map[string]any{
			"ID": id,
			"SMM": map[string]any{
				"Name": name,
			},
			"Foo": "bar",
		}),
		template.WithDataFn(func(_ context.Context, rr *types.ReconciliationRequest) (map[string]any, error) {
			return map[string]any{
				"Foo": "bar",
				"UID": rr.Instance.GetUID(),
			}, nil
		}),
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
				UID:  apytypes.UID(xid.New().String()),
			},
		},
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: ns,
				ServiceMesh: &infrav1.ServiceMeshSpec{
					ControlPlane: infrav1.ControlPlaneSpec{
						Name:      xid.New().String(),
						Namespace: xid.New().String(),
					},
				},
			},
		},
		Release:   common.Release{Name: cluster.OpenDataHub},
		Templates: []types.TemplateInfo{{FS: testFS, Path: "resources/smm-data.tmpl.yaml"}},
	}

	err = action(ctx, &rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.name == "%s"`, name),
			jq.Match(`.metadata.namespace == "%s"`, ns),
			jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, rr.DSCI.Spec.ServiceMesh.ControlPlane.Namespace),
			jq.Match(`.spec.controlPlaneRef.name == "%s"`, rr.DSCI.Spec.ServiceMesh.ControlPlane.Name),
			jq.Match(`.metadata.annotations."instance-name" == "%s"`, rr.Instance.GetName()),
			jq.Match(`.metadata.annotations."instance-id" == "%s"`, id),
			jq.Match(`.metadata.annotations."instance-uid" == "%s"`, rr.Instance.GetUID()),
			jq.Match(`.metadata.annotations."instance-foo" == "%s"`, "bar"),
		)),
	))
}

func TestRenderTemplateWithDataErr(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := template.NewAction(
		template.WithDataFn(func(_ context.Context, rr *types.ReconciliationRequest) (map[string]any, error) {
			return map[string]any{}, errors.New("compute-data-error")
		}),
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		},
		DSCI:      &dsciv1.DSCInitialization{},
		Release:   common.Release{Name: cluster.OpenDataHub},
		Templates: []types.TemplateInfo{{FS: testFS, Path: "resources/smm-data.tmpl.yaml"}},
	}

	err = action(ctx, &rr)

	g.Expect(err).Should(HaveOccurred())
}

func TestRenderTemplateWithCache(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := template.NewAction(
		template.WithCache(),
	)

	render.RenderedResourcesTotal.Reset()

	dsci := dsciv1.DSCInitialization{
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: ns,
			ServiceMesh: &infrav1.ServiceMeshSpec{
				ControlPlane: infrav1.ControlPlaneSpec{
					Name:      xid.New().String(),
					Namespace: xid.New().String(),
				},
			},
		},
	}

	for i := range 3 {
		d := componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}

		if i >= 1 {
			d.Generation = 1
		}

		rr := types.ReconciliationRequest{
			Client:    cl,
			Instance:  &d,
			DSCI:      &dsci,
			Release:   common.Release{Name: cluster.OpenDataHub},
			Templates: []types.TemplateInfo{{FS: testFS, Path: "resources/smm.tmpl.yaml"}},
		}

		err = action(ctx, &rr)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(And(
			HaveLen(1),
			HaveEach(And(
				jq.Match(`.metadata.namespace == "%s"`, ns),
				jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, rr.DSCI.Spec.ServiceMesh.ControlPlane.Namespace),
				jq.Match(`.spec.controlPlaneRef.name == "%s"`, rr.DSCI.Spec.ServiceMesh.ControlPlane.Name),
				jq.Match(`.metadata.annotations."instance-name" == "%s"`, rr.Instance.GetName()),
			)),
		))

		rc := testutil.ToFloat64(render.RenderedResourcesTotal)

		switch i {
		case 0:
			g.Expect(rc).Should(BeNumerically("==", 1))
		case 1:
			g.Expect(rc).Should(BeNumerically("==", 2))
		case 2:
			g.Expect(rc).Should(BeNumerically("==", 2))
		}
	}
}

func TestRenderTemplateWithGlob(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()
	id := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := template.NewAction()

	rrRef := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: id,
			},
		},
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: ns,
			},
		},
		Release: common.Release{Name: cluster.OpenDataHub},
	}

	t.Run("wildcard", func(t *testing.T) {
		g := NewWithT(t)

		rr := rrRef
		rr.Templates = []types.TemplateInfo{{FS: testFS, Path: "resources/g/*.yaml"}}

		err = action(ctx, &rr)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(And(
			HaveLen(2),
			HaveEach(And(
				jq.Match(`.metadata.namespace == "%s"`, rr.DSCI.Spec.ApplicationsNamespace),
				jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, rr.DSCI.Spec.ApplicationsNamespace),
				jq.Match(`.spec.controlPlaneRef.name == "%s"`, rr.Instance.GetName()),
			)),
		))
	})

	t.Run("named", func(t *testing.T) {
		g := NewWithT(t)

		rr := rrRef
		rr.Templates = []types.TemplateInfo{{FS: testFS, Path: "resources/g/sm-01.yaml"}}

		err = action(ctx, &rr)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(And(
			HaveLen(1),
			HaveEach(And(
				jq.Match(`.metadata.namespace == "%s"`, rr.DSCI.Spec.ApplicationsNamespace),
				jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, rr.DSCI.Spec.ApplicationsNamespace),
				jq.Match(`.spec.controlPlaneRef.name == "%s"`, rr.Instance.GetName()),
			)),
		))
	})
}

func TestRenderTemplateWithCustomInfo(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	ns := xid.New().String()
	id := xid.New().String()

	cl, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	action := template.NewAction(
		template.WithLabel("label-foo", "foo-label"),
		template.WithLabels(map[string]string{"labels-foo": "foo-labels"}),
		template.WithLabel("label-override", "foo-override"),
		template.WithAnnotation("annotation-foo", "foo-annotation"),
		template.WithAnnotations(map[string]string{"annotations-foo": "foo-annotations"}),
		template.WithAnnotation("annotation-override", "foo-override"),
	)

	rr := types.ReconciliationRequest{
		Client: cl,
		Instance: &componentApi.Dashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name: id,
			},
		},
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: ns,
			},
		},
		Release: common.Release{Name: cluster.OpenDataHub},
		Templates: []types.TemplateInfo{
			{
				FS:   testFS,
				Path: "resources/g/sm-01.yaml",
				Labels: map[string]string{
					"custom-label-foo": "label-01",
					"label-override":   "label-01",
				}},
			{
				FS:   testFS,
				Path: "resources/g/sm-02.yaml",
				Annotations: map[string]string{
					"custom-annotation-foo": "annotation-02",
					"annotation-override":   "annotation-02"},
			},
		},
	}

	err = action(ctx, &rr)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(rr.Resources).Should(And(
		HaveLen(2),
		HaveEach(And(
			jq.Match(`.metadata.namespace == "%s"`, rr.DSCI.Spec.ApplicationsNamespace),
			jq.Match(`.spec.controlPlaneRef.namespace == "%s"`, rr.DSCI.Spec.ApplicationsNamespace),
			jq.Match(`.spec.controlPlaneRef.name == "%s"`, rr.Instance.GetName()),
			jq.Match(`.metadata.labels."label-foo" == "foo-label"`),
			jq.Match(`.metadata.labels."labels-foo" == "foo-labels"`),
			jq.Match(`.metadata.labels | has("label-override")`),
			jq.Match(`.metadata.annotations."annotation-foo" == "foo-annotation"`),
			jq.Match(`.metadata.annotations."annotations-foo" == "foo-annotations"`),
			jq.Match(`.metadata.annotations | has("annotation-override")`),
		)),
	))

	g.Expect(rr.Resources[0]).Should(And(
		jq.Match(`.metadata.labels."custom-label-foo" == "label-01"`),
		jq.Match(`.metadata.labels."label-override" == "label-01"`),
	))

	g.Expect(rr.Resources[1]).Should(And(
		jq.Match(`.metadata.annotations."custom-annotation-foo" == "annotation-02"`),
		jq.Match(`.metadata.annotations."annotation-override" == "annotation-02"`),
	))
}
