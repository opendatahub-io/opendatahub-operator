package kustomize_test

import (
	"path"
	"testing"

	"github.com/rs/xid"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manifests/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const testEngineKustomization = `
apiVersion: kustomize.config.k8s.io/v1beta1
resources:
- test-engine-cm.yaml
`

const testEngineConfigMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-engine-cm
data:
  foo: bar
`

func TestEngine(t *testing.T) {
	g := NewWithT(t)
	id := xid.New().String()
	ns := xid.New().String()
	fs := filesys.MakeFsInMemory()

	e := kustomize.NewEngine(
		kustomize.WithEngineFS(fs),
	)

	_ = fs.MkdirAll(path.Join(id, kustomize.DefaultKustomizationFilePath))
	_ = fs.WriteFile(path.Join(id, kustomize.DefaultKustomizationFileName), []byte(testEngineKustomization))
	_ = fs.WriteFile(path.Join(id, "test-engine-cm.yaml"), []byte(testEngineConfigMap))

	r, err := e.Render(
		id,
		kustomize.WithNamespace(ns),
		kustomize.WithLabel("component.opendatahub.io/name", "foo"),
		kustomize.WithLabel("platform.opendatahub.io/namespace", ns),
		kustomize.WithAnnotations(map[string]string{
			"platform.opendatahub.io/release": "1.2.3",
			"platform.opendatahub.io/type":    "managed",
		}),
	)

	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(r).Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.namespace == "%s"`, ns),
			jq.Match(`.metadata.labels."component.opendatahub.io/name" == "%s"`, "foo"),
			jq.Match(`.metadata.labels."platform.opendatahub.io/namespace" == "%s"`, ns),
			jq.Match(`.metadata.annotations."platform.opendatahub.io/release" == "%s"`, "1.2.3"),
			jq.Match(`.metadata.annotations."platform.opendatahub.io/type" == "%s"`, "managed"),
		)),
	))
}

const testEngineKustomizationOrderLegacy = `
apiVersion: kustomize.config.k8s.io/v1beta1
sortOptions:
  order: legacy
resources:
- test-engine-cm.yaml
- test-engine-deployment.yaml
- test-engine-secrets.yaml
`
const testEngineKustomizationOrderLegacyCustom = `
apiVersion: kustomize.config.k8s.io/v1beta1
sortOptions:
  order: legacy
  legacySortOptions:
    orderFirst:
    - Secret
    - Deployment
    orderLast:
    - ConfigMap
resources:
- test-engine-cm.yaml
- test-engine-deployment.yaml
- test-engine-secrets.yaml
`

const testEngineKustomizationOrderFifo = `
apiVersion: kustomize.config.k8s.io/v1beta1
sortOptions:
  order: fifo
resources:
- test-engine-cm.yaml
- test-engine-deployment.yaml
- test-engine-secrets.yaml
`

const testEngineOrderConfigMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
data:
  foo: bar
`

//nolint:gosec
const testEngineOrderSecret = `
apiVersion: v1
kind: Secret
metadata:
  name: test-secrets
stringData:
  bar: baz
`

const testEngineOrderDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        volumeMounts:
        - name: config-volume
          mountPath: /etc/config
        - name: secrets-volume
          mountPath: /etc/secrets
      volumes:
        - name: config-volume
          configMap:
            name: test-cm
        - name: secrets-volume
          secret:
            name: test-secrets
`

func TestEngineOrder(t *testing.T) {
	root := xid.New().String()

	fs := filesys.MakeFsInMemory()

	kustomizations := map[string]string{
		"legacy":  testEngineKustomizationOrderLegacy,
		"ordered": testEngineKustomizationOrderLegacyCustom,
		"fifo":    testEngineKustomizationOrderFifo,
	}

	for k, v := range kustomizations {
		t.Run(k, func(t *testing.T) {
			g := NewWithT(t)

			e := kustomize.NewEngine(
				kustomize.WithEngineFS(fs),
			)

			_ = fs.MkdirAll(path.Join(root, kustomize.DefaultKustomizationFilePath))
			_ = fs.WriteFile(path.Join(root, kustomize.DefaultKustomizationFileName), []byte(v))
			_ = fs.WriteFile(path.Join(root, "test-engine-cm.yaml"), []byte(testEngineOrderConfigMap))
			_ = fs.WriteFile(path.Join(root, "test-engine-secrets.yaml"), []byte(testEngineOrderSecret))
			_ = fs.WriteFile(path.Join(root, "test-engine-deployment.yaml"), []byte(testEngineOrderDeployment))

			r, err := e.Render(root)

			g.Expect(err).NotTo(HaveOccurred())

			switch k {
			case "legacy":
				g.Expect(r).Should(And(
					HaveLen(3),
					jq.Match(`.[0] | .kind == "ConfigMap"`),
					jq.Match(`.[1] | .kind == "Secret"`),
					jq.Match(`.[2] | .kind == "Deployment"`),
				))
			case "ordered":
				g.Expect(r).Should(And(
					HaveLen(3),
					jq.Match(`.[0] | .kind == "Secret"`),
					jq.Match(`.[1] | .kind == "Deployment"`),
					jq.Match(`.[2] | .kind == "ConfigMap"`),
				))
			case "fifo":
				g.Expect(r).Should(And(
					HaveLen(3),
					jq.Match(`.[0] | .kind == "ConfigMap"`),
					jq.Match(`.[1] | .kind == "Deployment"`),
					jq.Match(`.[2] | .kind == "Secret"`),
				))
			}
		})
	}
}
