package v2_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestDataScienceClusterV2_ValidatingWebhook exercises the validating webhook logic for DataScienceCluster v2 resources.
// It verifies singleton enforcement, deletion rules, and Kueue managementState validation using table-driven tests and a fake client.
func TestDataScienceClusterV2_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	gvr := metav1.GroupVersionResource{
		Group:    gvk.DataScienceCluster.Group,
		Version:  gvk.DataScienceCluster.Version,
		Resource: "datascienceclusters",
	}

	withKueueState := func(state operatorv1.ManagementState) func(*dscv2.DataScienceCluster) {
		return func(dsc *dscv2.DataScienceCluster) {
			dsc.Spec.Components.Kueue.ManagementState = state
		}
	}

	cases := []struct {
		name         string
		existingObjs []client.Object
		req          admission.Request
		allowed      bool
	}{
		// Singleton and deletion cases
		{
			name:         "Allows creation if none exist",
			existingObjs: nil,
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test-create"), gvk.DataScienceCluster, gvr),
			allowed:      true,
		},
		{
			name:         "Denies creation if one already exists",
			existingObjs: []client.Object{envtestutil.NewDSC("existing")},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test-create"), gvk.DataScienceCluster, gvr),
			allowed:      false,
		},
		{
			name:         "Allows deletion always",
			existingObjs: nil,
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSC("test-delete"), gvk.DataScienceCluster, gvr),
			allowed:      true,
		},

		// Kueue managementState validation cases
		{
			name:    "Denies create with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test", withKueueState(operatorv1.Managed)), gvk.DataScienceCluster, gvr),
			allowed: false,
		},
		{
			name:    "Allows create with Kueue Unmanaged",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceCluster, gvr),
			allowed: true,
		},
		{
			name:    "Allows create with Kueue Removed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test", withKueueState(operatorv1.Removed)), gvk.DataScienceCluster, gvr),
			allowed: true,
		},
		{
			name:    "Denies update with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSC("test", withKueueState(operatorv1.Managed)), gvk.DataScienceCluster, gvr),
			allowed: false,
		},
		{
			name:    "Allows update with Kueue Unmanaged",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSC("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceCluster, gvr),
			allowed: true,
		},
		{
			name:    "Allows update with Kueue Removed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSC("test", withKueueState(operatorv1.Removed)), gvk.DataScienceCluster, gvr),
			allowed: true,
		},
		{
			name:    "Allows delete with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSC("test", withKueueState(operatorv1.Managed)), gvk.DataScienceCluster, gvr),
			allowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			objs := append([]client.Object{}, tc.existingObjs...)
			objs = append(objs, envtestutil.NewDSCI("dsci-for-dsc"))
			sch, err := scheme.New()
			g.Expect(err).ShouldNot(HaveOccurred())
			cli, err := fakeclient.New(fakeclient.WithObjects(objs...), fakeclient.WithScheme(sch))
			g.Expect(err).ShouldNot(HaveOccurred())
			validator := &v2webhook.Validator{
				Client:  cli,
				Name:    "test-v2",
				Decoder: admission.NewDecoder(sch),
			}
			resp := validator.Handle(ctx, tc.req)
			t.Logf("Admission response: Allowed=%v, Result=%+v", resp.Allowed, resp.Result)
			g.Expect(resp.Allowed).To(Equal(tc.allowed))
			if !tc.allowed {
				g.Expect(resp.Result.Message).ToNot(BeEmpty(), "Expected error message when request is denied")
			}
		})
	}
}
