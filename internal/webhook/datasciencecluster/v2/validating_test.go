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
		Group:    gvk.DataScienceClusterV2.Group,
		Version:  gvk.DataScienceClusterV2.Version,
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
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV2("test-create"), gvk.DataScienceClusterV2, gvr),
			allowed:      true,
		},
		{
			name:         "Denies creation if one already exists",
			existingObjs: []client.Object{envtestutil.NewDSCV2("existing")},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV2("test-create"), gvk.DataScienceClusterV2, gvr),
			allowed:      false,
		},
		{
			name:         "Allows deletion always",
			existingObjs: nil,
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSCV2("test-delete"), gvk.DataScienceClusterV2, gvr),
			allowed:      true,
		},

		// Kueue managementState validation cases
		{
			name:    "Denies create with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV2("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV2, gvr),
			allowed: false,
		},
		{
			name:    "Allows create with Kueue Unmanaged",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV2("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceClusterV2, gvr),
			allowed: true,
		},
		{
			name:    "Allows create with Kueue Removed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV2("test", withKueueState(operatorv1.Removed)), gvk.DataScienceClusterV2, gvr),
			allowed: true,
		},
		{
			name:    "Denies update with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSCV2("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV2, gvr),
			allowed: false,
		},
		{
			name:    "Allows update with Kueue Unmanaged",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSCV2("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceClusterV2, gvr),
			allowed: true,
		},
		{
			name:    "Allows update with Kueue Removed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSCV2("test", withKueueState(operatorv1.Removed)), gvk.DataScienceClusterV2, gvr),
			allowed: true,
		},
		{
			name:    "Allows delete with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSCV2("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV2, gvr),
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

func TestDataScienceClusterV2_NoModelsAsServiceWarning(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, err := scheme.New()
	g.Expect(err).NotTo(HaveOccurred())
	cli, err := fakeclient.New(fakeclient.WithScheme(sch))
	g.Expect(err).NotTo(HaveOccurred())
	validator := &v2webhook.Validator{Client: cli, Name: "test-v2", Decoder: admission.NewDecoder(sch)}
	gvr := metav1.GroupVersionResource{
		Group: gvk.DataScienceClusterV2.Group, Version: "v2", Resource: "datascienceclusters",
	}
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		for _, state := range []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed} {
			t.Run(string(operation)+"/"+string(state), func(t *testing.T) {
				t.Parallel()
				g := NewWithT(t)
				dsc := envtestutil.NewDSCV2("maas-warning")
				dsc.Spec.Components.Kserve.ModelsAsService.ManagementState = state //nolint:staticcheck
				req := envtestutil.NewAdmissionRequest(t, operation, dsc, gvk.DataScienceClusterV2, gvr)
				resp := validator.Handle(t.Context(), req)
				g.Expect(resp.Allowed).To(BeTrue())
				g.Expect(resp.Warnings).To(BeEmpty())
			})
		}
	}
}
