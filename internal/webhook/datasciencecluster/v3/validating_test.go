package v3_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestDataScienceClusterV3_ValidatingWebhook exercises the validating webhook logic for DataScienceCluster v3 resources.
// It verifies singleton enforcement, deletion rules, and Kueue managementState validation using table-driven tests and a fake client.
func TestDataScienceClusterV3_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	gvr := metav1.GroupVersionResource{
		Group:    gvk.DataScienceClusterV3.Group,
		Version:  gvk.DataScienceClusterV3.Version,
		Resource: "datascienceclusters",
	}

	withKueueState := func(state operatorv1.ManagementState) func(*dscv3.DataScienceCluster) {
		return func(dsc *dscv3.DataScienceCluster) {
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
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test-create"), gvk.DataScienceClusterV3, gvr),
			allowed:      true,
		},
		{
			name:         "Denies creation if one already exists",
			existingObjs: []client.Object{envtestutil.NewDSC("existing")},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test-create"), gvk.DataScienceClusterV3, gvr),
			allowed:      false,
		},
		{
			name:         "Allows deletion always",
			existingObjs: nil,
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSC("test-delete"), gvk.DataScienceClusterV3, gvr),
			allowed:      true,
		},

		// Kueue managementState validation cases
		{
			name:    "Denies create with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV3, gvr),
			allowed: false,
		},
		{
			name:    "Allows create with Kueue Unmanaged",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceClusterV3, gvr),
			allowed: true,
		},
		{
			name:    "Allows create with Kueue Removed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSC("test", withKueueState(operatorv1.Removed)), gvk.DataScienceClusterV3, gvr),
			allowed: true,
		},
		{
			name:    "Denies update with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSC("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV3, gvr),
			allowed: false,
		},
		{
			name:    "Allows update with Kueue Unmanaged",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSC("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceClusterV3, gvr),
			allowed: true,
		},
		{
			name:    "Allows update with Kueue Removed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSC("test", withKueueState(operatorv1.Removed)), gvk.DataScienceClusterV3, gvr),
			allowed: true,
		},
		{
			name:    "Allows delete with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSC("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV3, gvr),
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
			validator := &v3webhook.Validator{
				Client:  cli,
				Name:    "test-v3",
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

func TestDataScienceClusterV3_NoModelsAsServiceWarning(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	cli, err := fakeclient.New(fakeclient.WithScheme(sch))
	g.Expect(err).NotTo(HaveOccurred())
	validator := &v3webhook.Validator{Client: cli, Name: "test-v3", Decoder: admission.NewDecoder(sch)}
	gvr := metav1.GroupVersionResource{
		Group: gvk.DataScienceClusterV3.Group, Version: "v3", Resource: "datascienceclusters",
	}
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		for _, state := range []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed} {
			t.Run(string(operation)+"/"+string(state), func(t *testing.T) {
				t.Parallel()
				g := NewWithT(t)
				dsc := envtestutil.NewDSC("maas-warning")
				dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState = state
				req := envtestutil.NewAdmissionRequest(t, operation, dsc, gvk.DataScienceClusterV3, gvr)
				resp := validator.Handle(t.Context(), req)
				g.Expect(resp.Allowed).To(BeTrue())
				g.Expect(resp.Warnings).To(BeEmpty())
			})
		}
	}
}
