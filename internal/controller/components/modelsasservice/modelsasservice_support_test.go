//nolint:testpackage
package modelsasservice

import (
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/kustomize/api/provider"
	"sigs.k8s.io/kustomize/api/resmap"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	pkgtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

var rf = provider.NewDefaultDepProvider().GetResourceFactory()

func buildTestResMap(t *testing.T, yamls ...string) resmap.ResMap { //nolint:ireturn
	t.Helper()
	g := NewWithT(t)
	rm := resmap.New()
	for _, y := range yamls {
		res, err := rf.FromBytes([]byte(y))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rm.Append(res)).ShouldNot(HaveOccurred())
	}
	return rm
}

func TestRestoreGatewayNamespaceResources(t *testing.T) {
	g := NewWithT(t)

	rm := buildTestResMap(t,
		`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payload-processing
  namespace: redhat-ods-applications
`,
		`
apiVersion: v1
kind: Service
metadata:
  name: payload-processing
  namespace: redhat-ods-applications
`,
		`
apiVersion: v1
kind: ServiceAccount
metadata:
  name: payload-processing
  namespace: redhat-ods-applications
`,
		`
apiVersion: v1
kind: ConfigMap
metadata:
  name: payload-processing-plugins
  namespace: redhat-ods-applications
`,
		`
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: payload-processing
  namespace: redhat-ods-applications
`,
		// Unrelated resource that should NOT be moved
		`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: maas-controller
  namespace: redhat-ods-applications
`,
		// CRB with subjects in the wrong namespace
		`
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: payload-processing-reader
subjects:
- kind: ServiceAccount
  name: payload-processing
  namespace: redhat-ods-applications
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: payload-processing-reader
`,
	)

	err := restoreGatewayNamespaceResources(rm)
	g.Expect(err).ShouldNot(HaveOccurred())

	for _, res := range rm.Resources() {
		k := resourceKey{kind: res.GetKind(), name: res.GetName()}

		switch {
		case k == (resourceKey{kind: "Deployment", name: "maas-controller"}):
			g.Expect(res.GetNamespace()).To(Equal("redhat-ods-applications"),
				"unrelated resource should keep app namespace")

		case k.kind == "ClusterRoleBinding":
			m, err := res.Map()
			g.Expect(err).ShouldNot(HaveOccurred())
			subjects, ok := m["subjects"].([]any)
			g.Expect(ok).To(BeTrue(), "CRB should have subjects")
			for _, s := range subjects {
				subj, ok := s.(map[string]any)
				g.Expect(ok).To(BeTrue())
				g.Expect(subj["namespace"]).To(Equal(DefaultGatewayNamespace),
					"CRB subjects[].namespace should be restored to gateway namespace")
			}

		case gatewayNamespaceResources[k]:
			g.Expect(res.GetNamespace()).To(Equal(DefaultGatewayNamespace),
				"%s/%s should be moved to gateway namespace", k.kind, k.name)
		}
	}
}

func TestRestoreGatewayNamespaceResources_IgnoresNonAllowlisted(t *testing.T) {
	g := NewWithT(t)

	rm := buildTestResMap(t,
		`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: some-other-deployment
  namespace: redhat-ods-applications
`,
		// Same name but different kind — should NOT match
		`
apiVersion: v1
kind: ConfigMap
metadata:
  name: payload-processing
  namespace: redhat-ods-applications
`,
	)

	err := restoreGatewayNamespaceResources(rm)
	g.Expect(err).ShouldNot(HaveOccurred())

	for _, res := range rm.Resources() {
		g.Expect(res.GetNamespace()).To(Equal("redhat-ods-applications"),
			"%s/%s should not be moved", res.GetKind(), res.GetName())
	}
}

func TestRestoreCRBSubjectsNamespace(t *testing.T) {
	g := NewWithT(t)

	res, err := rf.FromBytes([]byte(`
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: payload-processing-reader
subjects:
- kind: ServiceAccount
  name: payload-processing
  namespace: wrong-namespace
- kind: ServiceAccount
  name: another-sa
  namespace: wrong-namespace
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: payload-processing-reader
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = restoreCRBSubjectsNamespace(res, "openshift-ingress")
	g.Expect(err).ShouldNot(HaveOccurred())

	m, err := res.Map()
	g.Expect(err).ShouldNot(HaveOccurred())
	subjects, ok := m["subjects"].([]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(subjects).To(HaveLen(2))

	for i, s := range subjects {
		subj, ok := s.(map[string]any)
		g.Expect(ok).To(BeTrue())
		g.Expect(subj["namespace"]).To(Equal("openshift-ingress"),
			"subjects[%d].namespace should be restored", i)
	}
}

func TestPayloadProcessingNetworkPolicy(t *testing.T) {
	g := NewWithT(t)

	labels := map[string]string{
		"opendatahub.io/component":  "true",
		"app.kubernetes.io/part-of": "modelsasservice",
	}

	np := payloadProcessingNetworkPolicy(labels)

	g.Expect(np.GetKind()).To(Equal("NetworkPolicy"))
	g.Expect(np.GetName()).To(Equal("payload-processing"))
	g.Expect(np.GetNamespace()).To(Equal(DefaultGatewayNamespace))

	npLabels := np.GetLabels()
	g.Expect(npLabels).To(HaveKeyWithValue("app", "payload-processing"))
	g.Expect(npLabels).To(HaveKeyWithValue("opendatahub.io/component", "true"))
	g.Expect(npLabels).To(HaveKeyWithValue("app.kubernetes.io/part-of", "modelsasservice"))

	spec, ok := np.Object["spec"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "spec should be a map")

	podSelector, ok := spec["podSelector"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "podSelector should be a map")
	matchLabels, ok := podSelector["matchLabels"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "matchLabels should be a map")
	g.Expect(matchLabels).To(HaveKeyWithValue("app", "payload-processing"))

	policyTypes, ok := spec["policyTypes"].([]any)
	g.Expect(ok).To(BeTrue(), "policyTypes should be an array")
	g.Expect(policyTypes).To(ConsistOf("Ingress", "Egress"))

	// Verify egress allows all outbound traffic
	egress, ok := spec["egress"].([]any)
	g.Expect(ok).To(BeTrue(), "egress should be an array")
	g.Expect(egress).To(HaveLen(1))
	g.Expect(egress[0]).To(Equal(map[string]any{}))

	// Verify ingress rules: gateway on 9004, monitoring on 9090
	ingress, ok := spec["ingress"].([]any)
	g.Expect(ok).To(BeTrue(), "ingress should be an array")
	g.Expect(ingress).To(HaveLen(2))
}

func TestRestoreCRBSubjectsNamespace_NoSubjects(t *testing.T) {
	g := NewWithT(t)

	res, err := rf.FromBytes([]byte(`
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: no-subjects-crb
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: some-role
`))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = restoreCRBSubjectsNamespace(res, "openshift-ingress")
	g.Expect(err).ShouldNot(HaveOccurred())
}

// createPolicyKustomizeBundle creates a temporary kustomize directory tree at
// <root>/maas/base/maas-controller/policies with a minimal AuthPolicy resource
// and returns the root path (to be used as ManifestsBasePath). Also creates
// a minimal params.env file under <root>/maas/overlays/odh/.
func createPolicyKustomizeBundle(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	policyDir := filepath.Join(root, MaasManifestContextDir, "base", "maas-controller", "policies")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("mkdir policies: %v", err)
	}

	authPolicy := `apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: deny-by-default
  namespace: placeholder
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: maas-default-gateway
`
	if err := os.WriteFile(filepath.Join(policyDir, "auth-policy.yaml"), []byte(authPolicy), 0o600); err != nil {
		t.Fatalf("write auth-policy.yaml: %v", err)
	}

	rateLimitPolicy := `apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: default-rate-limit
  namespace: placeholder
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: maas-default-gateway
`
	if err := os.WriteFile(filepath.Join(policyDir, "rate-limit-policy.yaml"), []byte(rateLimitPolicy), 0o600); err != nil {
		t.Fatalf("write rate-limit-policy.yaml: %v", err)
	}

	kustomization := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - auth-policy.yaml
  - rate-limit-policy.yaml
`
	if err := os.WriteFile(filepath.Join(policyDir, "kustomization.yaml"), []byte(kustomization), 0o600); err != nil {
		t.Fatalf("write kustomization.yaml: %v", err)
	}

	// Create a minimal params.env (required by the main install builder, but not by policy builder).
	paramsDir := filepath.Join(root, MaasManifestContextDir, BaseManifestsSourcePath)
	if err := os.MkdirAll(paramsDir, 0o755); err != nil {
		t.Fatalf("mkdir params: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paramsDir, "params.env"), []byte(""), 0o600); err != nil {
		t.Fatalf("write params.env: %v", err)
	}

	return root
}

func TestBuildMaasPolicyManifests(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	root := createPolicyKustomizeBundle(t)
	dsci := testDSCI()
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &pkgtypes.ReconciliationRequest{
		Client:            cli,
		ManifestsBasePath: root,
		Instance:          &componentApi.ModelsAsService{},
	}

	out, err := buildMaasPolicyManifests(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(out).To(HaveLen(2), "should render AuthPolicy and RateLimitPolicy")

	// Verify that resources have the correct namespace (gateway namespace).
	for _, obj := range out {
		g.Expect(obj.GetNamespace()).To(Equal(DefaultGatewayNamespace),
			"%s/%s should have the gateway namespace", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
	}

	// Verify that component labels are applied.
	for _, obj := range out {
		objLabels := obj.GetLabels()
		g.Expect(objLabels).To(HaveKeyWithValue(
			labels.ODH.Component(componentApi.ModelsAsServiceComponentName), labels.True),
			"component label should be set")
		g.Expect(objLabels).To(HaveKeyWithValue(
			labels.K8SCommon.PartOf, componentApi.ModelsAsServiceComponentName),
			"part-of label should be set")
	}

	// Verify resource kinds.
	kinds := make(map[string]bool)
	for _, obj := range out {
		kinds[obj.GetObjectKind().GroupVersionKind().Kind] = true
	}
	g.Expect(kinds).To(HaveKey("AuthPolicy"))
	g.Expect(kinds).To(HaveKey("RateLimitPolicy"))
}

func TestBuildMaasPolicyManifests_MissingBundle(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	root := t.TempDir()
	dsci := testDSCI()
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &pkgtypes.ReconciliationRequest{
		Client:            cli,
		ManifestsBasePath: root,
		Instance:          &componentApi.ModelsAsService{},
	}

	_, err = buildMaasPolicyManifests(ctx, rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("policy bundle not found"))
}

func TestBuildMaasPolicyManifests_EmptyManifestsBasePath(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	rr := &pkgtypes.ReconciliationRequest{
		ManifestsBasePath: "",
		Instance:          &componentApi.ModelsAsService{},
	}

	_, err := buildMaasPolicyManifests(ctx, rr)
	g.Expect(err).Should(HaveOccurred())
	g.Expect(err.Error()).Should(ContainSubstring("ManifestsBasePath is unset"))
}
