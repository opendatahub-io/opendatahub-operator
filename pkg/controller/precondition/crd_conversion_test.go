//nolint:testpackage // Exercises automatic phase selection by the factory.
package precondition

import (
	"context"
	"errors"
	"fmt"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

const conversionTestCRD = "platforms.config.opendatahub.io"

type conversionTestController struct {
	types.Controller

	reader client.Reader
}

//nolint:ireturn // Implements the controller interface.
func (c *conversionTestController) GetAPIReader() client.Reader {
	return c.reader
}

type conversionReadFunc struct {
	client.Reader

	get func(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error
}

func (r conversionReadFunc) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return r.get(ctx, key, obj, opts...)
}

func TestCRDConversion_Policies(t *testing.T) {
	strategies := []struct {
		name       string
		conversion *apiextensionsv1.CustomResourceConversion
	}{
		{name: "absent"},
		{name: "empty", conversion: &apiextensionsv1.CustomResourceConversion{}},
		{name: "None", conversion: &apiextensionsv1.CustomResourceConversion{Strategy: apiextensionsv1.NoneConverter}},
		{name: "Webhook", conversion: &apiextensionsv1.CustomResourceConversion{Strategy: apiextensionsv1.WebhookConverter}},
		{name: "unknown", conversion: &apiextensionsv1.CustomResourceConversion{Strategy: "Unknown"}},
	}
	versions := []struct {
		name   string
		served []bool
		count  int
	}{
		{name: "no served versions", served: []bool{false}, count: 0},
		{name: "one version", served: []bool{true}, count: 1},
		{name: "one served of two", served: []bool{true, false}, count: 1},
		{name: "two versions", served: []bool{true, true}, count: 2},
		{name: "two served of three", served: []bool{true, false, true}, count: 2},
		{name: "three versions", served: []bool{true, true, true}, count: 3},
	}
	for _, policy := range []CRDConversionPolicy{RequireWebhookAlways, RequireWebhookForMultipleServedVersions} {
		for _, strategy := range strategies {
			for _, version := range versions {
				t.Run(fmt.Sprintf("policy %d/%s/%s", policy, strategy.name, version.name), func(t *testing.T) {
					g := NewWithT(t)
					crd := &apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{Name: conversionTestCRD},
						Spec:       apiextensionsv1.CustomResourceDefinitionSpec{Conversion: strategy.conversion},
					}
					for i, served := range version.served {
						crd.Spec.Versions = append(crd.Spec.Versions, apiextensionsv1.CustomResourceDefinitionVersion{
							Name: fmt.Sprintf("v%d", i+11), Served: served,
						})
					}
					reads := 0
					reader := conversionReadFunc{get: func(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
						reads++
						g.Expect(key).To(Equal(client.ObjectKey{Name: conversionTestCRD}))
						destination, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
						g.Expect(ok).To(BeTrue())
						crd.DeepCopyInto(destination)
						return nil
					}}
					rr := &types.ReconciliationRequest{Controller: &conversionTestController{reader: reader}}
					pcs := []PreCondition{CRDConversion(conversionTestCRD, policy)}
					result, err := RunBeforeReconciliation(t.Context(), rr, pcs)
					g.Expect(err).NotTo(HaveOccurred())
					wantPass := strategy.name == "Webhook" || (policy == RequireWebhookForMultipleServedVersions && version.count <= 1)
					g.Expect(result.Pass).To(Equal(wantPass))
					if !wantPass {
						g.Expect(result.Message).To(ContainSubstring(conversionTestCRD))
						g.Expect(result.Message).To(ContainSubstring("requires Webhook conversion"))
						g.Expect(result.Message).To(ContainSubstring(fmt.Sprintf("served versions %d", version.count)))
					}
					g.Expect(reads).To(Equal(1))
					g.Expect(RunAll(t.Context(), rr, pcs)).To(BeFalse())
					g.Expect(reads).To(Equal(1), "ordinary execution must neither repeat the GET nor report conversion conditions")
					g.Expect(rr.Conditions).To(BeNil())
				})
			}
		}
	}
}

func TestCRDConversion_InvalidConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name       string
		crdName    string
		policy     CRDConversionPolicy
		controller types.Controller
		wantError  string
	}{
		{name: "empty name", policy: RequireWebhookAlways, wantError: "requires a CRD name"},
		{name: "invalid policy", crdName: conversionTestCRD, wantError: "invalid conversion policy"},
		{name: "missing controller", crdName: conversionTestCRD, policy: RequireWebhookAlways, wantError: "requires a controller"},
		{
			name: "nil APIReader", crdName: conversionTestCRD, policy: RequireWebhookAlways,
			controller: &conversionTestController{}, wantError: "requires an APIReader",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			rr := &types.ReconciliationRequest{Controller: tt.controller}
			result, err := RunBeforeReconciliation(t.Context(), rr, []PreCondition{CRDConversion(tt.crdName, tt.policy)})
			g.Expect(err).To(MatchError(ContainSubstring(tt.wantError)))
			g.Expect(result.Pass).To(BeFalse())
		})
	}
}

func TestCRDConversion_ReadErrors(t *testing.T) {
	resource := schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"}
	for _, readErr := range []error{
		k8serr.NewNotFound(resource, conversionTestCRD),
		k8serr.NewForbidden(resource, conversionTestCRD, errors.New("not permitted")),
		k8serr.NewInternalError(errors.New("API failure")),
		context.DeadlineExceeded,
		context.Canceled,
	} {
		t.Run(readErr.Error(), func(t *testing.T) {
			g := NewWithT(t)
			reader := conversionReadFunc{get: func(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
				return readErr
			}}
			rr := &types.ReconciliationRequest{Controller: &conversionTestController{reader: reader}}
			result, err := RunBeforeReconciliation(t.Context(), rr, []PreCondition{CRDConversion(conversionTestCRD, RequireWebhookAlways)})
			g.Expect(errors.Is(err, readErr)).To(BeTrue())
			g.Expect(err).To(MatchError(ContainSubstring(conversionTestCRD)))
			g.Expect(result.Pass).To(BeFalse())
		})
	}
}
