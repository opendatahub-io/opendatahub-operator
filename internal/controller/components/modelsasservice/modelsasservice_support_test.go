//nolint:testpackage
package modelsasservice

import (
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/kustomize/api/provider"
	"sigs.k8s.io/kustomize/api/resmap"
)

var rf = provider.NewDefaultDepProvider().GetResourceFactory()

func buildTestResMap(t *testing.T, yamls ...string) resmap.ResMap {
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
