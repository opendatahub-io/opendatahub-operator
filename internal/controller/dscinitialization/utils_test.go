package dscinitialization_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestPatchMonitoringNS(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	monitoringNS := xid.New().String()
	appsNS := xid.New().String()

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	dscInit := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: dsciv1.DSCInitializationSpec{
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: monitoringNS,
				},
			},
			ApplicationsNamespace: appsNS,
		},
	}

	err = dscinitialization.PatchMonitoringNS(ctx, cli, dscInit)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Assert that the namespace was created and has correct labels
	ns := &corev1.Namespace{}
	err = cli.Get(ctx, client.ObjectKey{Name: monitoringNS}, ns)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(ns.Labels).To(HaveKeyWithValue(labels.ODH.OwnedNamespace, labels.True))
	g.Expect(ns.Labels).To(HaveKeyWithValue(labels.SecurityEnforce, "baseline"))
	g.Expect(ns.Labels).To(HaveKeyWithValue(labels.ClusterMonitoring, labels.True))
}

func TestPatchExistingMonitoringNS(t *testing.T) {
	g := NewWithT(t)

	ctx := context.Background()
	monitoringNS := xid.New().String()
	appsNS := xid.New().String()

	cli, err := fakeclient.New(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: monitoringNS,
		},
	})

	g.Expect(err).ShouldNot(HaveOccurred())

	dscInit := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: dsciv1.DSCInitializationSpec{
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: monitoringNS,
				},
			},
			ApplicationsNamespace: appsNS,
		},
	}

	err = dscinitialization.PatchMonitoringNS(ctx, cli, dscInit)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Assert that the namespace was created and has correct labels
	ns := &corev1.Namespace{}
	err = cli.Get(ctx, client.ObjectKey{Name: monitoringNS}, ns)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(ns.Labels).To(HaveKeyWithValue(labels.ODH.OwnedNamespace, labels.True))
	g.Expect(ns.Labels).To(HaveKeyWithValue(labels.SecurityEnforce, "baseline"))
	g.Expect(ns.Labels).To(HaveKeyWithValue(labels.ClusterMonitoring, labels.True))
}
