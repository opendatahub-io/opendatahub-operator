package template_test

import (
	"context"
	"embed"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
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
		Release:   cluster.Release{Name: cluster.OpenDataHub},
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
		}),
	)

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
		Release:   cluster.Release{Name: cluster.OpenDataHub},
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
		)),
	))
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
			Release:   cluster.Release{Name: cluster.OpenDataHub},
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
