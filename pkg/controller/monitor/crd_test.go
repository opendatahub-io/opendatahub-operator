package monitor_test

import (
	"context"
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

var testRequiredGVK = schema.GroupVersionKind{
	Group:   "test.monitor.io",
	Version: "v1",
	Kind:    "TestRequired",
}

var testOtherRequiredGVK = schema.GroupVersionKind{
	Group:   "test.monitor.io",
	Version: "v1",
	Kind:    "TestOtherRequired",
}

const (
	testRequiredCRDName      = "testrequireds.test.monitor.io"
	testOtherRequiredCRDName = "testotherrequireds.test.monitor.io"
)

func TestCheckRequiredAPIs(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                   string
		objects                []client.Object
		apis                   []monitor.RequiredAPI
		expectedStatus         metav1.ConditionStatus
		expectedMsgContains    []string
		expectedMsgNotContains []string
	}{
		{
			name:                "missing CRD fails",
			apis:                []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testRequiredCRDName, "CRD not found"},
		},
		{
			name: "established CRD serving the declared version passes",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				[]apiextensionsv1.CustomResourceDefinitionCondition{crdCondition(apiextensionsv1.Established, apiextensionsv1.ConditionTrue)},
				servedVersion("v1", true),
			)},
			apis:           []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus: metav1.ConditionTrue,
		},
		{
			// A CRD under deletion keeps Established=True and is still readable, so
			// reporting it as absent would hide the real reason the API is unusable.
			name: "terminating CRD fails and is not reported as missing",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				[]apiextensionsv1.CustomResourceDefinitionCondition{
					crdCondition(apiextensionsv1.Established, apiextensionsv1.ConditionTrue),
					crdCondition(apiextensionsv1.Terminating, apiextensionsv1.ConditionTrue),
				},
				servedVersion("v1", true),
			)},
			apis:                   []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus:         metav1.ConditionFalse,
			expectedMsgContains:    []string{testRequiredCRDName, "CRD is terminating"},
			expectedMsgNotContains: []string{"not found"},
		},
		{
			// Still installing or rejected outright cannot be told apart here, so the
			// caller must requeue rather than declare the dependency broken.
			name: "CRD without Established condition is indeterminate",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				nil,
				servedVersion("v1", true),
			)},
			apis:                []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus:      metav1.ConditionUnknown,
			expectedMsgContains: []string{testRequiredCRDName, "CRD is not established"},
		},
		{
			name: "CRD with Established=False is indeterminate",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				[]apiextensionsv1.CustomResourceDefinitionCondition{crdCondition(apiextensionsv1.Established, apiextensionsv1.ConditionFalse)},
				servedVersion("v1", true),
			)},
			apis:                []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus:      metav1.ConditionUnknown,
			expectedMsgContains: []string{testRequiredCRDName, "CRD is not established"},
		},
		{
			name: "unserved version fails even when CRD is not established",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				[]apiextensionsv1.CustomResourceDefinitionCondition{crdCondition(apiextensionsv1.Established, apiextensionsv1.ConditionFalse)},
				servedVersion("v1", false),
			)},
			apis:                []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testRequiredCRDName, "does not serve version v1"},
		},
		{
			name: "declared version absent from spec.versions fails",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				[]apiextensionsv1.CustomResourceDefinitionCondition{crdCondition(apiextensionsv1.Established, apiextensionsv1.ConditionTrue)},
				servedVersion("v2", true),
			)},
			apis:                []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testRequiredCRDName, "does not serve version v1"},
		},
		{
			// served is a property of spec.versions, not a status condition: a version
			// can be declared and still be unreachable.
			name: "declared version present but not served fails",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				[]apiextensionsv1.CustomResourceDefinitionCondition{crdCondition(apiextensionsv1.Established, apiextensionsv1.ConditionTrue)},
				servedVersion("v1", false),
			)},
			apis:                []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}},
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{testRequiredCRDName, "does not serve version v1"},
		},
		{
			name: "empty declared version skips the served-version check",
			objects: []client.Object{newTestCRD(testRequiredCRDName,
				[]apiextensionsv1.CustomResourceDefinitionCondition{crdCondition(apiextensionsv1.Established, apiextensionsv1.ConditionTrue)},
				servedVersion("v2", true),
			)},
			apis:           []monitor.RequiredAPI{{CRDName: testRequiredCRDName}},
			expectedStatus: metav1.ConditionTrue,
		},
		{
			// A definite failure outranks an indeterminate one, but both are reported.
			name: "failure and indeterminate together report False with both findings",
			objects: []client.Object{newTestCRD(testOtherRequiredCRDName,
				nil,
				servedVersion("v1", true),
			)},
			apis: []monitor.RequiredAPI{
				{CRDName: testRequiredCRDName, GVK: testRequiredGVK},
				{CRDName: testOtherRequiredCRDName, GVK: testOtherRequiredGVK},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedMsgContains: []string{
				testRequiredCRDName + ": CRD not found",
				testOtherRequiredCRDName + ": CRD is not established",
			},
		},
		{
			name: "only indeterminate findings report Unknown",
			objects: []client.Object{
				newTestCRD(testRequiredCRDName, nil, servedVersion("v1", true)),
				newTestCRD(testOtherRequiredCRDName, nil, servedVersion("v1", true)),
			},
			apis: []monitor.RequiredAPI{
				{CRDName: testRequiredCRDName, GVK: testRequiredGVK},
				{CRDName: testOtherRequiredCRDName, GVK: testOtherRequiredGVK},
			},
			expectedStatus: metav1.ConditionUnknown,
			expectedMsgContains: []string{
				testRequiredCRDName + ": CRD is not established",
				testOtherRequiredCRDName + ": CRD is not established",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			cli, err := fakeclient.New(fakeclient.WithObjects(tt.objects...))
			g.Expect(err).NotTo(HaveOccurred())

			result, err := monitor.CheckRequiredAPIs(ctx, cli, tt.apis)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.ConditionStatus()).To(Equal(tt.expectedStatus))

			for _, s := range tt.expectedMsgContains {
				g.Expect(result.Message).To(ContainSubstring(s))
			}
			for _, s := range tt.expectedMsgNotContains {
				g.Expect(result.Message).NotTo(ContainSubstring(s))
			}
		})
	}
}

// TestCheckRequiredAPIs_EstablishedByAPIServer exercises the check against a CRD
// whose Established condition was set by a real EstablishingController rather than
// hand-written into the object.
func TestCheckRequiredAPIs_EstablishedByAPIServer(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, testRequiredGVK, "testrequireds", "testrequired", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)

	result, err := monitor.CheckRequiredAPIs(ctx, cli, []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.ConditionStatus()).To(Equal(metav1.ConditionTrue))
	g.Expect(result.Message).To(BeEmpty())
}

func TestCheckRequiredAPIs_InvalidInput(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		apis            []monitor.RequiredAPI
		expectedErrText string
	}{
		{
			name:            "empty API list",
			apis:            nil,
			expectedErrText: "empty required API list",
		},
		{
			name:            "empty CRDName",
			apis:            []monitor.RequiredAPI{{GVK: testRequiredGVK}},
			expectedErrText: "CRDName must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			cli, err := fakeclient.New()
			g.Expect(err).NotTo(HaveOccurred())

			_, err = monitor.CheckRequiredAPIs(ctx, cli, tt.apis)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tt.expectedErrText))
		})
	}
}

// A read that fails is not evidence about the CRD, so it surfaces as an error the
// caller can retry rather than as a False result.
func TestCheckRequiredAPIs_TransientAPIError(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return errors.New("simulated transient API error")
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	result, err := monitor.CheckRequiredAPIs(ctx, cli, []monitor.RequiredAPI{{CRDName: testRequiredCRDName, GVK: testRequiredGVK}})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to check CRD presence"))
	g.Expect(err.Error()).To(ContainSubstring(testRequiredCRDName))
	g.Expect(result).To(Equal(monitor.CheckResult{}))
}

func TestCheckRequiredAPIs_MissingCRDOutranksReadError(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	apiErr := errors.New("simulated API timeout")

	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, reader client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if key.Name == testOtherRequiredCRDName {
				return apiErr
			}

			return reader.Get(ctx, key, obj, opts...)
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	result, err := monitor.CheckRequiredAPIs(ctx, cli, []monitor.RequiredAPI{
		{CRDName: testRequiredCRDName, GVK: testRequiredGVK},
		{CRDName: testOtherRequiredCRDName, GVK: testOtherRequiredGVK},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.ConditionStatus()).To(Equal(metav1.ConditionFalse))
	g.Expect(result.Message).To(ContainSubstring(testRequiredCRDName + ": CRD not found"))
	g.Expect(result.Message).To(ContainSubstring("simulated API timeout"))
}

func TestCheckRequiredAPIs_MultipleReadErrorsRemainErrors(t *testing.T) {
	g := NewWithT(t)
	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, key client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return errors.New("read failed for " + key.Name)
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())

	result, err := monitor.CheckRequiredAPIs(t.Context(), cli, []monitor.RequiredAPI{
		{CRDName: testRequiredCRDName, GVK: testRequiredGVK},
		{CRDName: testOtherRequiredCRDName, GVK: testOtherRequiredGVK},
	})
	g.Expect(result).To(Equal(monitor.CheckResult{}))
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("read failed for " + testRequiredCRDName))
	g.Expect(err.Error()).To(ContainSubstring("read failed for " + testOtherRequiredCRDName))
}

// Test helpers

func newTestCRD(
	name string,
	conditions []apiextensionsv1.CustomResourceDefinitionCondition,
	versions ...apiextensionsv1.CustomResourceDefinitionVersion,
) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Versions: versions,
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			Conditions: conditions,
		},
	}
}

func crdCondition(
	condType apiextensionsv1.CustomResourceDefinitionConditionType,
	status apiextensionsv1.ConditionStatus,
) apiextensionsv1.CustomResourceDefinitionCondition {
	return apiextensionsv1.CustomResourceDefinitionCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
	}
}

func servedVersion(name string, served bool) apiextensionsv1.CustomResourceDefinitionVersion {
	return apiextensionsv1.CustomResourceDefinitionVersion{
		Name:   name,
		Served: served,
	}
}
