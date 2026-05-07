//nolint:testpackage
package datasciencecluster

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func makeDeployment(name string, existingEnv ...map[string]any) unstructured.Unstructured {
	containers := []any{
		map[string]any{
			"name":  "manager",
			"image": "registry.example.com/module:latest",
		},
	}

	if len(existingEnv) > 0 {
		envSlice := make([]any, 0, len(existingEnv))
		for _, e := range existingEnv {
			envSlice = append(envSlice, e)
		}

		c, ok := containers[0].(map[string]any)
		if ok {
			c["env"] = envSlice
		}
	}

	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "opendatahub",
			},
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"containers": containers,
					},
				},
			},
		},
	}
}

func makeConfigMap(name string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "opendatahub",
			},
		},
	}
}

func getContainerEnvByName(obj *unstructured.Unstructured, containerName string) []any {
	containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	for _, c := range containers {
		if cm, ok := c.(map[string]any); ok {
			if name, _ := cm["name"].(string); name == containerName {
				env, _ := cm["env"].([]any)
				return env
			}
		}
	}

	return nil
}

func getContainerEnv(obj *unstructured.Unstructured) []any {
	return getContainerEnvByName(obj, "manager")
}

func envNames(env []any) []string {
	names := make([]string, 0, len(env))
	for _, e := range env {
		if em, ok := e.(map[string]any); ok {
			if name, ok := em["name"].(string); ok {
				names = append(names, name)
			}
		}
	}

	return names
}

func envValue(env []any, name string) string {
	for _, e := range env {
		if em, ok := e.(map[string]any); ok {
			if n, _ := em["name"].(string); n == name {
				v, _ := em["value"].(string)
				return v
			}
		}
	}

	return ""
}

func TestInjectModuleEnvNoop(t *testing.T) {
	g := NewWithT(t)

	dep := makeDeployment("my-operator")
	rr := &odhtype.ReconciliationRequest{
		Resources: []unstructured.Unstructured{dep},
	}

	err := injectModuleEnv(context.Background(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	env := getContainerEnv(&rr.Resources[0])
	g.Expect(env).Should(BeNil())
}

func TestInjectModuleEnvRelatedImages(t *testing.T) {
	g := NewWithT(t)

	t.Setenv("RELATED_IMAGE_TRAINER", "registry.example.com/trainer@sha256:abc")
	t.Setenv("RELATED_IMAGE_PROXY", "registry.example.com/proxy@sha256:def")

	dep := makeDeployment("trainer-operator")
	cm := makeConfigMap("trainer-config")

	rr := &odhtype.ReconciliationRequest{
		Resources: []unstructured.Unstructured{dep, cm},
		ModuleEnvInjection: &odhtype.ModuleEnvInjection{
			RelatedImages: []string{
				"RELATED_IMAGE_TRAINER",
				"RELATED_IMAGE_PROXY",
				"RELATED_IMAGE_MISSING",
			},
			ApplicationsNamespace: "opendatahub",
		},
	}

	err := injectModuleEnv(context.Background(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	env := getContainerEnv(&rr.Resources[0])
	g.Expect(envNames(env)).Should(ConsistOf(
		"RELATED_IMAGE_TRAINER",
		"RELATED_IMAGE_PROXY",
		"APPLICATIONS_NAMESPACE",
	))
	g.Expect(envValue(env, "RELATED_IMAGE_TRAINER")).Should(Equal("registry.example.com/trainer@sha256:abc"))
	g.Expect(envValue(env, "RELATED_IMAGE_PROXY")).Should(Equal("registry.example.com/proxy@sha256:def"))
	g.Expect(envValue(env, "APPLICATIONS_NAMESPACE")).Should(Equal("opendatahub"))

	cmObj := rr.Resources[1]
	g.Expect(cmObj.GetKind()).Should(Equal("ConfigMap"))
}

func TestInjectModuleEnvSkipsExistingVars(t *testing.T) {
	g := NewWithT(t)

	t.Setenv("RELATED_IMAGE_TRAINER", "registry.example.com/trainer@sha256:new")

	dep := makeDeployment("trainer-operator", map[string]any{
		"name":  "RELATED_IMAGE_TRAINER",
		"value": "registry.example.com/trainer@sha256:original",
	})

	rr := &odhtype.ReconciliationRequest{
		Resources: []unstructured.Unstructured{dep},
		ModuleEnvInjection: &odhtype.ModuleEnvInjection{
			RelatedImages:         []string{"RELATED_IMAGE_TRAINER"},
			ApplicationsNamespace: "opendatahub",
		},
	}

	err := injectModuleEnv(context.Background(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	env := getContainerEnv(&rr.Resources[0])
	g.Expect(envValue(env, "RELATED_IMAGE_TRAINER")).Should(Equal("registry.example.com/trainer@sha256:original"))
	g.Expect(envNames(env)).Should(ContainElement("APPLICATIONS_NAMESPACE"))
}

func makeMultiContainerDeployment(name string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "opendatahub",
			},
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"containers": []any{
							map[string]any{
								"name":  "sidecar",
								"image": "registry.example.com/sidecar:latest",
							},
							map[string]any{
								"name":  "manager",
								"image": "registry.example.com/module:latest",
							},
						},
					},
				},
			},
		},
	}
}

func TestInjectModuleEnvTargetsManagerContainer(t *testing.T) {
	g := NewWithT(t)

	t.Setenv("RELATED_IMAGE_TRAINER", "registry.example.com/trainer@sha256:abc")

	dep := makeMultiContainerDeployment("trainer-operator")

	rr := &odhtype.ReconciliationRequest{
		Resources: []unstructured.Unstructured{dep},
		ModuleEnvInjection: &odhtype.ModuleEnvInjection{
			RelatedImages:         []string{"RELATED_IMAGE_TRAINER"},
			ApplicationsNamespace: "opendatahub",
		},
	}

	err := injectModuleEnv(context.Background(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	managerEnv := getContainerEnvByName(&rr.Resources[0], "manager")
	g.Expect(envNames(managerEnv)).Should(ConsistOf(
		"RELATED_IMAGE_TRAINER",
		"APPLICATIONS_NAMESPACE",
	))

	sidecarEnv := getContainerEnvByName(&rr.Resources[0], "sidecar")
	g.Expect(sidecarEnv).Should(BeNil())
}

func TestInjectModuleEnvEmptyNamespace(t *testing.T) {
	g := NewWithT(t)

	dep := makeDeployment("my-operator")

	rr := &odhtype.ReconciliationRequest{
		Resources: []unstructured.Unstructured{dep},
		ModuleEnvInjection: &odhtype.ModuleEnvInjection{
			ApplicationsNamespace: "",
		},
	}

	err := injectModuleEnv(context.Background(), rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	env := getContainerEnv(&rr.Resources[0])
	g.Expect(env).Should(BeNil())
}
