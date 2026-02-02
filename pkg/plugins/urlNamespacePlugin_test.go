package plugins_test

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("URL Namespace plugin", func() {
	It("Should transform URLs with opendatahub namespace to target namespace", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: kuadrant.io/v1beta3
kind: AuthPolicy
metadata:
  name: gateway-auth-policy
  namespace: istio-system
spec:
  rules:
    metadata:
      matchedTier:
        http:
          url: https://maas-api.opendatahub.svc.cluster.local:8443/v1/tiers/lookup
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPlugin("redhat-ods-applications")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		expected := `
apiVersion: kuadrant.io/v1beta3
kind: AuthPolicy
metadata:
  name: gateway-auth-policy
  namespace: istio-system
spec:
  rules:
    metadata:
      matchedTier:
        http:
          url: https://maas-api.redhat-ods-applications.svc.cluster.local:8443/v1/tiers/lookup
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should transform URLs with custom source namespace", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  api-url: http://service.custom-namespace.svc.cluster.local:8080/api
  other-key: "some value"
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPluginWithSource("custom-namespace", "target-ns")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		expected := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  api-url: http://service.target-ns.svc.cluster.local:8080/api
  other-key: "some value"
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should transform multiple URLs in the same resource", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  primary-api: http://api1.opendatahub.svc.cluster.local:8080/v1
  secondary-api: http://api2.opendatahub.svc.cluster.local:9090/v2
  external-url: https://example.com/api
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPlugin("my-namespace")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		expected := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  primary-api: http://api1.my-namespace.svc.cluster.local:8080/v1
  secondary-api: http://api2.my-namespace.svc.cluster.local:9090/v2
  external-url: https://example.com/api
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should handle nested structures", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  template:
    spec:
      containers:
      - name: app
        env:
        - name: API_URL
          value: http://backend.opendatahub.svc.cluster.local:8080
        - name: OTHER_VAR
          value: "test"
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPlugin("prod-namespace")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		expected := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  template:
    spec:
      containers:
      - name: app
        env:
        - name: API_URL
          value: http://backend.prod-namespace.svc.cluster.local:8080
        - name: OTHER_VAR
          value: "test"
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should not transform when source equals target", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  url: http://service.opendatahub.svc.cluster.local:8080
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPlugin("opendatahub")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		expected := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  url: http://service.opendatahub.svc.cluster.local:8080
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should not transform URLs with different namespace patterns", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  url: http://service.other-namespace.svc.cluster.local:8080
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPlugin("target-ns")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		// URL should remain unchanged because it uses "other-namespace", not "opendatahub"
		expected := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  url: http://service.other-namespace.svc.cluster.local:8080
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should handle empty target namespace (no transformation)", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  url: http://service.opendatahub.svc.cluster.local:8080
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPlugin("")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		// Should remain unchanged
		expected := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  url: http://service.opendatahub.svc.cluster.local:8080
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should handle URL in string with additional content", func() {
		res, err := factory.FromBytes([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  complex: "Connect to http://api.opendatahub.svc.cluster.local:8080/v1/endpoint for API access"
`))
		Expect(err).NotTo(HaveOccurred())

		plugin := plugins.CreateURLNamespaceTransformerPlugin("rhoai")
		err = plugin.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		expected := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  complex: "Connect to http://api.rhoai.svc.cluster.local:8080/v1/endpoint for API access"
`
		Expect(res.MustYaml()).To(MatchYAML(expected))
	})
})
