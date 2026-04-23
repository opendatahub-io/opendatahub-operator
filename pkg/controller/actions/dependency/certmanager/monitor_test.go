package certmanager_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestCRDPredicate verifies that CRDPredicate matches the three core cert-manager CRDs
// and rejects unrelated objects.
func TestCRDPredicate(t *testing.T) {
	pred := certmanager.CRDPredicate()

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

// TestMonitorCRDs verifies that MonitorCRDs pre-configures monitoring for the three core
// cert-manager CRDs.
//
// Each subtest uses its own envtest instance rather than sharing one across subtests.
// HasCRD relies on the REST mapper, whose discovery cache refreshes asynchronously after
// CRD deletion. A shared instance cannot guarantee the mapper reflects zero CRDs at the
// start of the "absent CRDs" case when other subtests registered CRDs beforehand.
func TestMonitorCRDs(t *testing.T) {
	tests := []struct {
		name                   string
		setupCRDs              func(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT)
		expectedAvailable      bool
		expectedMsgContains    []string
		expectedMsgNotContains []string
	}{
		{
			name:              "absent CRDs yield degraded",
			setupCRDs:         nil,
			expectedAvailable: false,
			expectedMsgContains: []string{
				gvk.CertManagerCertificate.Kind,
				gvk.CertManagerIssuer.Kind,
				gvk.CertManagerClusterIssuer.Kind,
			},
		},
		{
			name: "present CRDs yield healthy",
			setupCRDs: func(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
				t.Helper()
				_, err := envTest.RegisterCertManagerCRDs(ctx)
				g.Expect(err).NotTo(HaveOccurred())
			},
			expectedAvailable: true,
		},
		{
			name: "mix of present and absent CRDs",
			setupCRDs: func(t *testing.T, g *WithT, ctx context.Context, envTest *envt.EnvT) {
				t.Helper()
				_, err := envTest.RegisterCRD(ctx, gvk.CertManagerCertificate, "certificates", "certificate", apiextensionsv1.NamespaceScoped)
				g.Expect(err).NotTo(HaveOccurred())
				// Issuer and ClusterIssuer CRDs intentionally not registered
			},
			expectedAvailable:      false,
			expectedMsgContains:    []string{gvk.CertManagerIssuer.Kind, gvk.CertManagerClusterIssuer.Kind},
			expectedMsgNotContains: []string{gvk.CertManagerCertificate.Kind + ": CRD not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			envTest, err := envt.New()
			g.Expect(err).NotTo(HaveOccurred())
			t.Cleanup(func() { _ = envTest.Stop() })

			ctx := context.Background()
			cli := envTest.Client()

			if tt.setupCRDs != nil {
				tt.setupCRDs(t, g, ctx, envTest)
			}

			instance := &scheme.TestPlatformObject{ObjectMeta: metav1.ObjectMeta{Name: xid.New().String()}}
			condManager := cond.NewManager(instance, status.ConditionDependenciesAvailable)
			rr := &types.ReconciliationRequest{Client: cli, Instance: instance, Conditions: condManager}

			action := dependency.NewAction(certmanager.MonitorCRDs())
			err = action(ctx, rr)
			g.Expect(err).NotTo(HaveOccurred())

			gotCond := condManager.GetCondition(status.ConditionDependenciesAvailable)
			g.Expect(gotCond).NotTo(BeNil())

			if tt.expectedAvailable {
				g.Expect(gotCond.Status).To(Equal(metav1.ConditionTrue))
			} else {
				g.Expect(gotCond.Status).To(Equal(metav1.ConditionFalse))
				for _, s := range tt.expectedMsgContains {
					g.Expect(gotCond.Message).To(ContainSubstring(s))
				}
				for _, s := range tt.expectedMsgNotContains {
					g.Expect(gotCond.Message).NotTo(ContainSubstring(s))
				}
			}
		})
	}
}
