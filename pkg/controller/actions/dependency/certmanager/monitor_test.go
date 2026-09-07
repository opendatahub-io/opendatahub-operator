//nolint:testpackage
package certmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/xid"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestCRDPredicate(t *testing.T) {
	pred := crdPredicate()

	makeCRD := func(name string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apiextensions.k8s.io",
			Version: "v1",
			Kind:    "CustomResourceDefinition",
		})
		u.SetName(name)
		return u
	}

	tests := []struct {
		name     string
		crdName  string
		expected bool
	}{
		{name: "Certificate CRD matches", crdName: "certificates.cert-manager.io", expected: true},
		{name: "Issuer CRD matches", crdName: "issuers.cert-manager.io", expected: true},
		{name: "ClusterIssuer CRD matches", crdName: "clusterissuers.cert-manager.io", expected: true},
		// The OpenShift health CRD may appear after the controller started; without an
		// event for it the dynamic CertManager/cluster watch is never registered.
		{name: "OpenShift cert-manager operator CRD matches", crdName: "certmanagers.operator.openshift.io", expected: true},
		{name: "unrelated CRD does not match", crdName: "widgets.other.io", expected: false},
		{name: "other cert-manager CRD does not match", crdName: "certificaterequests.cert-manager.io", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			obj := makeCRD(tt.crdName)
			g.Expect(pred.Create(event.CreateEvent{Object: obj})).To(Equal(tt.expected))
			g.Expect(pred.Update(event.UpdateEvent{ObjectNew: obj})).To(Equal(tt.expected))
			g.Expect(pred.Delete(event.DeleteEvent{Object: obj})).To(Equal(tt.expected))
			g.Expect(pred.Generic(event.GenericEvent{Object: obj})).To(Equal(tt.expected))
		})
	}
}

func TestWatchedCRDs(t *testing.T) {
	g := NewWithT(t)

	g.Expect(watchedCRDs()).To(ConsistOf(
		gvk.CertManagerCertificateCRDName,
		gvk.CertManagerIssuerCRDName,
		gvk.CertManagerClusterIssuerCRDName,
		gvk.CertManagerOperatorCRDName,
	))
}

func TestRequiredAPIList(t *testing.T) {
	g := NewWithT(t)

	// Every required API must carry its version so the served-version check runs,
	// and its CRD name must be in the Kubernetes "<plural>.<group>" form.
	for _, api := range requiredAPIList {
		g.Expect(api.CRDName).To(HaveSuffix("." + api.GVK.Group))
		g.Expect(api.GVK.Version).NotTo(BeEmpty())
	}

	g.Expect(requiredAPIList).To(ConsistOf(
		monitor.RequiredAPI{CRDName: gvk.CertManagerCertificateCRDName, GVK: gvk.CertManagerCertificate},
		monitor.RequiredAPI{CRDName: gvk.CertManagerIssuerCRDName, GVK: gvk.CertManagerIssuer},
		monitor.RequiredAPI{CRDName: gvk.CertManagerClusterIssuerCRDName, GVK: gvk.CertManagerClusterIssuer},
	))
}

func TestCertManagerConditionFilter(t *testing.T) {
	for _, tc := range []struct {
		name          string
		conditionType string
		status        string
		wantDegraded  bool
	}{
		{name: "deployment degraded is checked as a required condition", conditionType: "cert-manager-controller-deploymentDegraded", status: "True"},
		{name: "static resource degraded", conditionType: "cert-manager-networkpolicy-static-resources-Degraded", status: "True", wantDegraded: true},
		{name: "other degraded condition", conditionType: "other-Degraded", status: "True", wantDegraded: true},
		{name: "degraded false", conditionType: "cert-manager-controller-deploymentDegraded", status: "False"},
		{name: "available false is handled by required conditions", conditionType: "cert-manager-controller-deploymentAvailable", status: "False"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			NewWithT(t).Expect(certManagerConditionFilter(tc.conditionType, tc.status)).To(Equal(tc.wantDegraded))
		})
	}
}

// Both probes contribute to DependenciesAvailable; dropping one silently would
// leave a class of cert-manager breakage undetected.
func TestPreConditions(t *testing.T) {
	g := NewWithT(t)

	g.Expect(preConditions()).To(HaveLen(2))
}

func TestRequeueIfDependenciesIndeterminate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      metav1.ConditionStatus
		wantRequeue bool
	}{
		{name: "healthy dependency", status: metav1.ConditionTrue},
		{name: "failed dependency is event-driven", status: metav1.ConditionFalse},
		{name: "indeterminate dependency", status: metav1.ConditionUnknown, wantRequeue: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
			manager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
			manager.Mark(status.ConditionDependenciesAvailable, tc.status)
			rr := &types.ReconciliationRequest{Instance: instance, Conditions: manager}

			err := requeueIfDependenciesIndeterminate(t.Context(), rr)
			if !tc.wantRequeue {
				g.Expect(err).NotTo(HaveOccurred())
				return
			}

			var requeueErr odherrors.RequeueAfterError
			g.Expect(errors.As(err, &requeueErr)).To(BeTrue())
			g.Expect(requeueErr.After).To(Equal(30 * time.Second))
		})
	}
}

func TestOperatorHealthPreCondition_CRDFoundDuringDiscoveryLag(t *testing.T) {
	g := NewWithT(t)
	crd := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: gvk.CertManagerOperatorCRDName}}
	cli, err := fakeclient.New(
		fakeclient.WithObjects(crd),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, reader client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, isCRD := obj.(*apiextensionsv1.CustomResourceDefinition); isCRD {
					return reader.Get(ctx, key, obj, opts...)
				}
				return &meta.NoKindMatchError{GroupKind: gvk.CertManagerV1Alpha1.GroupKind(), SearchedVersions: []string{gvk.CertManagerV1Alpha1.Version}}
			},
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	manager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: manager}
	precondition.RunAll(t.Context(), rr, []precondition.PreCondition{operatorHealthPreCondition()})

	got := manager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionUnknown))
}

// Each subtest uses its own envtest instance rather than sharing one across subtests,
// so a CRD registered by one case cannot leak into the "absent CRDs" case.
func TestRequiredAPIsPreCondition(t *testing.T) {
	tests := []struct {
		name                   string
		setupCRDs              func(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT)
		expectedStatus         metav1.ConditionStatus
		expectedMsgContains    []string
		expectedMsgNotContains []string
	}{
		{
			name:           "absent CRDs yield failure",
			setupCRDs:      nil,
			expectedStatus: metav1.ConditionFalse,
			expectedMsgContains: []string{
				gvk.CertManagerCertificateCRDName,
				gvk.CertManagerIssuerCRDName,
				gvk.CertManagerClusterIssuerCRDName,
			},
		},
		{
			name: "present CRDs yield pass",
			setupCRDs: func(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
				t.Helper()
				_, err := envTest.RegisterCertManagerCRDs(ctx)
				g.Expect(err).NotTo(HaveOccurred())
			},
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "mix of present and absent CRDs",
			setupCRDs: func(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
				t.Helper()
				// Issuer and ClusterIssuer CRDs intentionally not registered
				_, err := envTest.RegisterCRD(ctx, gvk.CertManagerCertificate, "certificates", "certificate", apiextensionsv1.NamespaceScoped)
				g.Expect(err).NotTo(HaveOccurred())
			},
			expectedStatus:         metav1.ConditionFalse,
			expectedMsgContains:    []string{gvk.CertManagerIssuerCRDName, gvk.CertManagerClusterIssuerCRDName},
			expectedMsgNotContains: []string{gvk.CertManagerCertificateCRDName},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			envTest, err := envt.New()
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = envTest.Stop() })

			ctx := context.Background()

			if tt.setupCRDs != nil {
				tt.setupCRDs(t, g, ctx, envTest)
			}

			instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
			condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{Client: envTest.Client(), Instance: instance, Conditions: condManager}

			precondition.RunAll(ctx, rr, []precondition.PreCondition{precondition.MonitorAPIs(requiredAPIList)})

			got := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(got).NotTo(BeNil())
			g.Expect(got.Status).To(Equal(tt.expectedStatus))

			for _, s := range tt.expectedMsgContains {
				g.Expect(got.Message).To(ContainSubstring(s))
			}
			for _, s := range tt.expectedMsgNotContains {
				g.Expect(got.Message).NotTo(ContainSubstring(s))
			}
		})
	}
}

// On a community cert-manager install the OpenShift health CRD does not exist.
// The check must be skipped, not reported as a failure.
func TestOperatorHealthPreConditionSkippedWithoutCRD(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
	condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
	rr := &types.ReconciliationRequest{Client: envTest.Client(), Instance: instance, Conditions: condManager}

	precondition.RunAll(ctx, rr, []precondition.PreCondition{operatorHealthPreCondition()})

	got := condManager.GetCondition(status.ConditionDependenciesAvailable)
	g.Expect(got).NotTo(BeNil())
	g.Expect(got.Status).To(Equal(metav1.ConditionTrue))
}

// On OpenShift the health CR is the signal a CRD-presence check cannot give:
// it reports a cert-manager that is installed but not working.
func TestOperatorHealthPreConditionWithCRD(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	cli := envTest.Client()

	crd, err := envTest.RegisterCRD(ctx, gvk.CertManagerV1Alpha1, "certmanagers", "certmanager",
		apiextensionsv1.ClusterScoped, envt.WithPermissiveSchema())
	g.Expect(err).NotTo(HaveOccurred())
	envt.CleanupDelete(t, g, ctx, cli, crd)
	g.Expect(crd.Name).To(Equal(gvk.CertManagerOperatorCRDName))

	tests := []struct {
		name                string
		conditions          []metav1.Condition
		createCR            bool
		expectedStatus      metav1.ConditionStatus
		expectedMsgContains []string
	}{
		{
			name: "healthy operator passes",
			conditions: []metav1.Condition{
				{Type: "cert-manager-controller-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-webhook-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-cainjector-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-controller-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
				{Type: "cert-manager-webhook-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
				{Type: "cert-manager-cainjector-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:       true,
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "degraded operator fails",
			conditions: []metav1.Condition{
				{Type: "cert-manager-controller-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-webhook-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-cainjector-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-controller-deploymentDegraded", Status: metav1.ConditionTrue, Reason: "Broken", Message: "controller crashlooping"},
				{Type: "cert-manager-webhook-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
				{Type: "cert-manager-cainjector-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"cert-manager-controller-deploymentDegraded=True", "controller crashlooping"},
		},
		{
			// An operator that never reported Available is not healthy just because
			// it never reported Degraded either.
			name: "operator that never reported Available is Unknown",
			conditions: []metav1.Condition{
				{Type: "cert-manager-controller-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionUnknown,
			expectedMsgContains: []string{"required condition cert-manager-controller-deploymentAvailable not found"},
		},
		{
			name: "a component that is not Available fails",
			conditions: []metav1.Condition{
				{Type: "cert-manager-controller-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-webhook-deploymentAvailable", Status: metav1.ConditionTrue, Reason: "AsExpected"},
				{Type: "cert-manager-cainjector-deploymentAvailable", Status: metav1.ConditionFalse, Reason: "Deploying", Message: "Waiting for Deployment"},
				{Type: "cert-manager-controller-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
				{Type: "cert-manager-webhook-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
				{Type: "cert-manager-cainjector-deploymentDegraded", Status: metav1.ConditionFalse, Reason: "AsExpected"},
			},
			createCR:            true,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"cert-manager-cainjector-deploymentAvailable=False", "expected cert-manager-cainjector-deploymentAvailable=True"},
		},
		{
			// The CRD is installed, so the operator is expected; a missing CR is a failure.
			name:                "missing health CR fails",
			createCR:            false,
			expectedStatus:      metav1.ConditionFalse,
			expectedMsgContains: []string{"operator CR not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			if tt.createCR {
				cr := testf.NewUnstructuredCR(certManagerOperatorCRName, "", gvk.CertManagerV1Alpha1)
				g.Expect(testf.SetTypedConditions(cr, tt.conditions)).NotTo(HaveOccurred())
				g.Expect(testf.CreateAndUpdateStatus(ctx, cli, cr)).NotTo(HaveOccurred())
				envt.CleanupDelete(t, g, ctx, cli, cr)
			}

			instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
			condManager := cond.NewManager(instance, status.ConditionTypeReady, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

			precondition.RunAll(ctx, rr, []precondition.PreCondition{operatorHealthPreCondition()})

			got := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(got).NotTo(BeNil())
			g.Expect(got.Status).To(Equal(tt.expectedStatus))

			for _, s := range tt.expectedMsgContains {
				g.Expect(got.Message).To(ContainSubstring(s))
			}
		})
	}
}
