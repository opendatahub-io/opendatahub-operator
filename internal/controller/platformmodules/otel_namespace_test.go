//nolint:testpackage // Exercises package-private webhook namespace resolution.
package platformmodules

import (
	"context"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func collectorWebhook(name, namespace string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name: "vopentelemetrycollector.kb.io",
			ClientConfig: admissionregistrationv1.WebhookClientConfig{Service: &admissionregistrationv1.ServiceReference{
				Name: "opentelemetry-operator-webhook-service", Namespace: namespace,
			}},
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Rule: admissionregistrationv1.Rule{APIGroups: []string{"opentelemetry.io"}, Resources: []string{"opentelemetrycollectors"}},
			}},
		}},
	}
}

func TestResolveOTelOperatorNamespace(t *testing.T) {
	unrelated := collectorWebhook("other", "wrong")
	unrelated.Webhooks[0].ClientConfig.Service.Name = "other-service"
	for _, tc := range []struct {
		name, want, wantError string
		objects               []client.Object
	}{
		{name: "not installed"},
		{
			name: "active webhook", want: "openshift-operators",
			objects: []client.Object{collectorWebhook("collector-create", "openshift-operators"), collectorWebhook("collector-delete", "openshift-operators")},
		},
		{
			name: "conflicting installations", wantError: "multiple namespaces",
			objects: []client.Object{collectorWebhook("collector-one", "openshift-operators"), collectorWebhook("collector-two", "other-operators")},
		},
		{name: "unrelated webhook", objects: []client.Object{unrelated}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			cli, err := fakeclient.New(fakeclient.WithObjects(tc.objects...))
			g.Expect(err).NotTo(HaveOccurred())
			got, err := resolveOTelOperatorNamespace(context.Background(), cli)
			if tc.wantError != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantError)))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tc.want))
		})
	}
}

func TestOTelCollectorWebhookWatch(t *testing.T) {
	for _, representation := range []struct {
		name         string
		unstructured bool
	}{
		{name: "typed"},
		{name: "unstructured", unstructured: true},
	} {
		t.Run(representation.name, func(t *testing.T) {
			g := NewWithT(t)
			asWatchObject := func(configuration *admissionregistrationv1.ValidatingWebhookConfiguration) client.Object {
				if !representation.unstructured {
					return configuration
				}
				obj, err := resources.ToUnstructured(configuration)
				g.Expect(err).NotTo(HaveOccurred())
				return obj
			}
			matching := asWatchObject(collectorWebhook("collector-admission", "openshift-operators"))
			other := collectorWebhook("other", "wrong")
			other.Webhooks[0].ClientConfig.Service.Name = "other-service"
			unrelated := asWatchObject(other)
			predicate := otelCollectorWebhookPredicate()
			for _, tc := range []struct {
				name string
				run  func() bool
				want bool
			}{
				{name: "matching create", run: func() bool { return predicate.Create(event.CreateEvent{Object: matching}) }, want: true},
				{name: "unrelated create", run: func() bool { return predicate.Create(event.CreateEvent{Object: unrelated}) }},
				{name: "matching delete", run: func() bool { return predicate.Delete(event.DeleteEvent{Object: matching}) }, want: true},
				{name: "unrelated delete", run: func() bool { return predicate.Delete(event.DeleteEvent{Object: unrelated}) }},
				{
					name: "update changes target namespace", want: true,
					run: func() bool {
						return predicate.Update(event.UpdateEvent{
							ObjectOld: asWatchObject(collectorWebhook("collector-admission", "old-operators")),
							ObjectNew: asWatchObject(collectorWebhook("collector-admission", "new-operators")),
						})
					},
				},
				{name: "update removes collector webhook", run: func() bool { return predicate.Update(event.UpdateEvent{ObjectOld: matching, ObjectNew: unrelated}) }, want: true},
				{name: "update adds collector webhook", run: func() bool { return predicate.Update(event.UpdateEvent{ObjectOld: unrelated, ObjectNew: matching}) }, want: true},
				{name: "unrelated update", run: func() bool { return predicate.Update(event.UpdateEvent{ObjectOld: unrelated, ObjectNew: unrelated}) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					NewWithT(t).Expect(tc.run()).To(Equal(tc.want))
				})
			}
			g.Expect(monitoringModuleRequests(t.Context(), matching)).To(Equal([]reconcile.Request{{
				NamespacedName: k8stypes.NamespacedName{Name: serviceApi.MonitoringServiceName},
			}}))
		})
	}
}

func TestBuildModulePlatformContextMonitoringInputs(t *testing.T) {
	for _, tc := range []struct {
		name, module, want string
		conflicting        bool
		serviceMonitorCRD  bool
		wantServiceMonitor bool
		wantError          bool
	}{
		{name: "monitoring", module: serviceApi.MonitoringServiceName, want: "openshift-operators"},
		{
			name: "monitoring with RHOBS API", module: serviceApi.MonitoringServiceName, want: "openshift-operators",
			serviceMonitorCRD: true, wantServiceMonitor: true,
		},
		{name: "monitoring fails closed on ambiguous webhooks", module: serviceApi.MonitoringServiceName, conflicting: true, wantError: true},
		{name: "other modules skip monitoring inputs", module: "dashboard", conflicting: true, serviceMonitorCRD: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			dsci := &dsciv2.DSCInitialization{ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"}}
			dsci.Spec.ApplicationsNamespace = "apps"
			objects := []client.Object{dsci, collectorWebhook("collector-one", "openshift-operators")}
			if tc.conflicting {
				objects = append(objects, collectorWebhook("collector-two", "other-operators"))
			}
			if tc.serviceMonitorCRD {
				objects = append(objects, &apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: gvk.ServiceMonitorCRDName},
				})
			}
			cli, err := fakeclient.New(
				fakeclient.WithObjects(objects...),
				fakeclient.WithGVKs(fakeclient.GVKMapping{GVK: gvk.ServiceMonitor, Scope: meta.RESTScopeNamespace}),
			)
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtype.ReconciliationRequest{Client: cli, Instance: &configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: tc.module}}}
			got, err := buildModulePlatformContext(t.Context(), rr)
			if tc.wantError {
				g.Expect(err).To(MatchError(ContainSubstring("multiple namespaces")))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got.OTelOperatorNamespace).To(Equal(tc.want))
			g.Expect(got.MetricsServiceMonitorEnabled).To(Equal(tc.wantServiceMonitor))
		})
	}
}
