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
