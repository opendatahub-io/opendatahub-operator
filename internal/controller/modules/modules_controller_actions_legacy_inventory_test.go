//nolint:testpackage // Exercises the internal 3.6 migration inventory.
package modules

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestListLegacyModuleResources(t *testing.T) {
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "platform"}}
	// Even for a shared name, eligibility follows ownership and release metadata.
	legacy := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "odh-db-internal-postgres", Namespace: "apps",
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(platform, gvk.Platform)},
		Labels:          map[string]string{labels.PlatformPartOf: "dashboard"},
		Annotations: map[string]string{
			annotations.InstanceUID:     string(platform.UID),
			annotations.InstanceName:    platform.Name,
			annotations.PlatformVersion: "3.6.0",
			annotations.PlatformType:    string(cluster.XKS),
		},
	}}
	adopted := legacy.DeepCopy()
	adopted.Name = "adopted"
	adopted.OwnerReferences[0] = *metav1.NewControllerRef(
		&configApi.PlatformModule{ObjectMeta: metav1.ObjectMeta{Name: "dashboard", UID: "tracker"}}, gvk.PlatformModule)
	current := legacy.DeepCopy()
	current.Name = "current"
	current.Annotations[annotations.PlatformVersion] = "3.7.0"

	for _, tc := range []struct {
		name    string
		gvk     schema.GroupVersionKind
		err     error
		wantErr bool
	}{
		{
			name: "external API absent", gvk: gvk.CertManagerCertificate,
			err: &meta.NoKindMatchError{GroupKind: gvk.CertManagerCertificate.GroupKind()},
		},
		{
			name: "built-in API absent", gvk: gvk.Deployment,
			err: &meta.NoKindMatchError{GroupKind: gvk.Deployment.GroupKind()},
		},
		{
			name: "API temporarily unavailable", gvk: gvk.CertManagerCertificate,
			err:     k8serr.NewServiceUnavailable("try again"),
			wantErr: true,
		},
		{
			name: "validating webhooks outside the 3.6 allowlist", gvk: gvk.ValidatingWebhookConfiguration,
			err: k8serr.NewServiceUnavailable("webhook API unavailable"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			cli, err := fakeclient.New(fakeclient.WithObjects(legacy.DeepCopy(), adopted.DeepCopy(), current.DeepCopy()),
				fakeclient.WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, cli client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						kind := list.GetObjectKind().GroupVersionKind()
						switch kind {
						case tc.gvk:
							return tc.err
						case gvk.ConfigMap:
							return cli.List(ctx, list, opts...)
						default:
							return nil
						}
					},
				}))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := listLegacyModuleResources(t.Context(), &types.ReconciliationRequest{
				Instance: platform, Client: cli, Release: common.Release{Name: cluster.XKS},
			})
			if tc.wantErr {
				g.Expect(err).To(MatchError(ContainSubstring(tc.err.Error())))
				g.Expect(got).To(BeNil())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(HaveLen(1))
			g.Expect(got[0].GetName()).To(Equal(legacy.Name))
		})
	}
}

func TestLegacyInventoryListsAllKindsWithCachedClient(t *testing.T) {
	g := NewWithT(t)
	et, err := envt.New(envt.WithCRDPaths(t.TempDir()), envt.WithManager())
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { g.Expect(et.Stop()).To(Succeed()) })

	expected := make([]schema.GroupVersionKind, 0, len(legacyModuleResourceKinds))
	expected = append(expected,
		gvk.ConfigMap,
		gvk.Service,
		gvk.ServiceAccount,
		gvk.Deployment,
		gvk.Role,
		gvk.RoleBinding,
		gvk.ClusterRole,
		gvk.ClusterRoleBinding,
		gvk.MutatingWebhookConfiguration,
		gvk.ValidatingAdmissionPolicy,
		gvk.ValidatingAdmissionPolicyBinding,
		gvk.NetworkPolicy,
	)
	// Use the real API list kinds even though the scanner passes the item kind to the cache.
	for _, api := range []struct {
		kind                       schema.GroupVersionKind
		plural, singular, listKind string
		scope                      apiextensionsv1.ResourceScope
	}{
		{gvk.CertManagerCertificate, "certificates", "certificate", "CertificateList", apiextensionsv1.NamespaceScoped},
		{gvk.CertManagerIssuer, "issuers", "issuer", "IssuerList", apiextensionsv1.NamespaceScoped},
		{gvk.CoreosPrometheusRule, "prometheusrules", "prometheusrule", "PrometheusRuleList", apiextensionsv1.NamespaceScoped},
		{gvk.CoreosServiceMonitor, "servicemonitors", "servicemonitor", "ServiceMonitorList", apiextensionsv1.NamespaceScoped},
		{gvk.ServiceMonitor, "servicemonitors", "servicemonitor", "ServiceMonitorList", apiextensionsv1.NamespaceScoped},
		// Envtest serves the OpenShift built-in API through a stand-in CRD.
		{gvk.SecurityContextConstraints, "securitycontextconstraints", "securitycontextconstraints", "SecurityContextConstraintsList", apiextensionsv1.ClusterScoped},
	} {
		_, err := et.RegisterCRD(t.Context(), api.kind, api.plural, api.singular, api.scope,
			func(crd *apiextensionsv1.CustomResourceDefinition) {
				crd.Spec.Names.ListKind = api.listKind
			})
		g.Expect(err).NotTo(HaveOccurred())
		expected = append(expected, api.kind)
	}

	et.StartManager(t, t.Context())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	mgr := et.Manager()
	g.Expect(mgr.GetCache().WaitForCacheSync(ctx)).To(BeTrue())
	cli := &legacyInventoryListClient{Client: mgr.GetClient()}
	_, err = listLegacyModuleResources(ctx, &types.ReconciliationRequest{
		Client: cli, Instance: &configApi.Platform{},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cli.listed).To(ConsistOf(expected))
}

type legacyInventoryListClient struct {
	client.Client

	listed []schema.GroupVersionKind
}

func (c *legacyInventoryListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if err := c.Client.List(ctx, list, opts...); err != nil {
		return err
	}
	c.listed = append(c.listed, list.GetObjectKind().GroupVersionKind())
	return nil
}
