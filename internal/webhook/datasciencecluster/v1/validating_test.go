package v1_test

import (
	"reflect"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	v1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/dsc/compare"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestDataScienceClusterV1_ValidatingWebhook exercises the validating webhook logic for DataScienceCluster v1 resources.
// It verifies singleton enforcement, deletion rules, and Kueue managementState validation using table-driven tests and a fake client.
func TestDataScienceClusterV1_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	gvr := metav1.GroupVersionResource{
		Group:    gvk.DataScienceClusterV1.Group,
		Version:  gvk.DataScienceClusterV1.Version,
		Resource: "datascienceclusters",
	}

	withKueueState := func(state operatorv1.ManagementState) func(*dscv1.DataScienceCluster) {
		return func(dsc *dscv1.DataScienceCluster) {
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
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV1("test-create"), gvk.DataScienceClusterV1, gvr),
			allowed:      true,
		},
		{
			name:         "Denies creation if one already exists",
			existingObjs: []client.Object{envtestutil.NewDSCV1("existing")},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV1("test-create"), gvk.DataScienceClusterV1, gvr),
			allowed:      false,
		},
		{
			name:         "Allows deletion always",
			existingObjs: nil,
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSCV1("test-delete"), gvk.DataScienceClusterV1, gvr),
			allowed:      true,
		},

		// Kueue managementState validation cases
		{
			name:    "Denies create with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV1("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV1, gvr),
			allowed: false,
		},
		{
			name:    "Allows create with Kueue Unmanaged",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV1("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceClusterV1, gvr),
			allowed: true,
		},
		{
			name:    "Allows create with Kueue Removed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCV1("test", withKueueState(operatorv1.Removed)), gvk.DataScienceClusterV1, gvr),
			allowed: true,
		},
		{
			name:         "Denies update with Kueue Managed",
			existingObjs: []client.Object{envtestutil.NewDSC("test", envtestutil.WithAllV2OnlyComponentsRemoved())},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSCV1("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV1, gvr),
			allowed:      false,
		},
		{
			name:         "Allows update with Kueue Unmanaged",
			existingObjs: []client.Object{envtestutil.NewDSC("test", envtestutil.WithAllV2OnlyComponentsRemoved())},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSCV1("test", withKueueState(operatorv1.Unmanaged)), gvk.DataScienceClusterV1, gvr),
			allowed:      true,
		},
		{
			name:         "Allows update with Kueue Removed",
			existingObjs: []client.Object{envtestutil.NewDSC("test", envtestutil.WithAllV2OnlyComponentsRemoved())},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Update, envtestutil.NewDSCV1("test", withKueueState(operatorv1.Removed)), gvk.DataScienceClusterV1, gvr),
			allowed:      true,
		},
		{
			name:    "Allows delete with Kueue Managed",
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSCV1("test", withKueueState(operatorv1.Managed)), gvk.DataScienceClusterV1, gvr),
			allowed: true,
		},

		// V2-only component protection cases
		{
			name: "Allows v1 update when no v2-only components are Managed",
			existingObjs: []client.Object{
				envtestutil.NewDSC("test-dsc",
					envtestutil.WithAllV2OnlyComponentsRemoved(),
					func(dsc *dscv2.DataScienceCluster) {
						dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
					}),
			},
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc", func(dsc *dscv1.DataScienceCluster) {
					dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Removed
				}),
				gvk.DataScienceClusterV1, gvr),
			allowed: true,
		},
		{
			name: "Allows v1 update when AIPipelines is Managed (v1 equivalent component)",
			existingObjs: []client.Object{
				envtestutil.NewDSC("test-dsc",
					envtestutil.WithAllV2OnlyComponentsRemoved(),
					func(dsc *dscv2.DataScienceCluster) {
						dsc.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed
					}),
			},
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc", func(dsc *dscv1.DataScienceCluster) {
					dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
				}),
				gvk.DataScienceClusterV1, gvr),
			allowed: true,
		},
		{
			name: "Denies v1 update when Trainer is Managed",
			existingObjs: []client.Object{
				envtestutil.NewDSC("test-dsc",
					envtestutil.WithAllV2OnlyComponentsRemoved(),
					envtestutil.WithTrainerManaged()),
			},
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc", func(dsc *dscv1.DataScienceCluster) {
					dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
				}),
				gvk.DataScienceClusterV1, gvr),
			allowed: false,
		},
		{
			name: "Denies v1 update when MLflowOperator is Managed",
			existingObjs: []client.Object{
				envtestutil.NewDSC("test-dsc",
					envtestutil.WithAllV2OnlyComponentsRemoved(),
					envtestutil.WithMLflowOperatorManaged()),
			},
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc", func(dsc *dscv1.DataScienceCluster) {
					dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
				}),
				gvk.DataScienceClusterV1, gvr),
			allowed: false,
		},
		{
			name: "Denies v1 update when SparkOperator is Managed",
			existingObjs: []client.Object{
				envtestutil.NewDSC("test-dsc",
					envtestutil.WithAllV2OnlyComponentsRemoved(),
					envtestutil.WithSparkOperatorManaged()),
			},
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc", func(dsc *dscv1.DataScienceCluster) {
					dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
				}),
				gvk.DataScienceClusterV1, gvr),
			allowed: false,
		},
		{
			name: "Denies v1 update when multiple v2-only components are Managed",
			existingObjs: []client.Object{
				envtestutil.NewDSC("test-dsc",
					envtestutil.WithTrainerManaged(),
					envtestutil.WithMLflowOperatorManaged(),
					envtestutil.WithSparkOperatorManaged()),
			},
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc", func(dsc *dscv1.DataScienceCluster) {
					dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
				}),
				gvk.DataScienceClusterV1, gvr),
			allowed: false,
		},
		{
			name:         "Denies v1 update when DSC does not exist (fail-closed security)",
			existingObjs: []client.Object{},
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc-nonexistent", func(dsc *dscv1.DataScienceCluster) {
					dsc.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
				}),
				gvk.DataScienceClusterV1, gvr),
			allowed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			objs := append([]client.Object{}, tc.existingObjs...)
			objs = append(objs, envtestutil.NewDSCIV1("dsci-for-dsc"))
			sch, err := scheme.New()
			g.Expect(err).ShouldNot(HaveOccurred())
			cli, err := fakeclient.New(fakeclient.WithObjects(objs...), fakeclient.WithScheme(sch))
			g.Expect(err).ShouldNot(HaveOccurred())
			validator := &v1webhook.Validator{
				Client:  cli,
				Name:    "test-v1",
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

// TestDataScienceClusterV1_ValidatingWebhook_AllV2OnlyComponents ensures webhook coverage for ALL v2-only components.
// This test dynamically iterates over all v2-only components (discovered via reflection) and verifies
// that the webhook blocks v1 API updates when any of them is Managed. This provides a safety net
// that automatically includes new v2-only components without requiring test updates.
func TestDataScienceClusterV1_ValidatingWebhook_AllV2OnlyComponents(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	gvr := metav1.GroupVersionResource{
		Group:    gvk.DataScienceClusterV1.Group,
		Version:  gvk.DataScienceClusterV1.Version,
		Resource: "datascienceclusters",
	}

	// Iterate over all v2-only component field names
	v2OnlyFieldNames := compare.GetV2OnlyComponentFieldNames()
	if len(v2OnlyFieldNames) == 0 {
		t.Skip("No v2-only components found - skipping test")
	}

	for _, fieldName := range v2OnlyFieldNames {
		t.Run("Denies v1 update when "+fieldName+" is Managed", func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			// Create a DSC with this specific v2-only component set to Managed via reflection
			dsc := envtestutil.NewDSC("test-dsc", func(dsc *dscv2.DataScienceCluster) {
				componentsValue := reflect.ValueOf(&dsc.Spec.Components).Elem()
				field := componentsValue.FieldByName(fieldName)
				if !field.IsValid() {
					t.Fatalf("Field %s not found in Components struct", fieldName)
				}
				managementStateField := field.FieldByName("ManagementState")
				if !managementStateField.IsValid() || !managementStateField.CanSet() {
					t.Fatalf("ManagementState field not found or not settable for %s", fieldName)
				}
				managementStateField.Set(reflect.ValueOf(operatorv1.Managed))
			})

			// Create v1 update request (attempting to modify a v1-compatible field)
			req := envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCV1("test-dsc", func(dscv1 *dscv1.DataScienceCluster) {
					dscv1.Spec.Components.Dashboard.ManagementState = operatorv1.Managed
				}),
				gvk.DataScienceClusterV1, gvr)

			// Setup webhook validator
			objs := []client.Object{dsc, envtestutil.NewDSCIV1("dsci-for-dsc")}
			sch, err := scheme.New()
			g.Expect(err).ShouldNot(HaveOccurred())
			cli, err := fakeclient.New(fakeclient.WithObjects(objs...), fakeclient.WithScheme(sch))
			g.Expect(err).ShouldNot(HaveOccurred())
			validator := &v1webhook.Validator{
				Client:  cli,
				Name:    "test-v1",
				Decoder: admission.NewDecoder(sch),
			}

			// Execute webhook and verify it denies the request
			resp := validator.Handle(ctx, req)
			g.Expect(resp.Allowed).To(BeFalse(), "Expected webhook to deny v1 update when %s is Managed", fieldName)
			g.Expect(resp.Result.Message).To(ContainSubstring("v2-only components"), "Expected error message about v2-only components")
			t.Logf("✓ Webhook correctly blocked v1 update for v2-only component: %s", fieldName)
		})
	}
}
