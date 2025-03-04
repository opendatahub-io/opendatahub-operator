//nolint:testpackage
package monitoring

import (
	"testing"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

func TestExtractContent(t *testing.T) {
	t.Run("valid content", func(t *testing.T) {
		g := NewWithT(t)

		validConfig := PrometheusConfig{
			RuleFiles: []string{"rule1.yml", "rule2.yml"},
			Others:    map[string]any{"extra": "data"},
		}

		yamlData, err := yaml.Marshal(validConfig)
		g.Expect(err).ShouldNot(HaveOccurred())

		cfg := PrometheusConfig{}
		err = resources.ExtractContent(
			&corev1.ConfigMap{
				Data: map[string]string{prometheusConfigurationEntry: string(yamlData)},
			},
			prometheusConfigurationEntry,
			&cfg,
		)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cfg.RuleFiles).Should(ConsistOf("rule1.yml", "rule2.yml"))
		g.Expect(cfg.Others).Should(HaveKeyWithValue("extra", "data"))
	})

	t.Run("empty content", func(t *testing.T) {
		g := NewWithT(t)

		cfg := PrometheusConfig{}
		err := resources.ExtractContent(
			&corev1.ConfigMap{Data: map[string]string{}},
			prometheusConfigurationEntry,
			&cfg,
		)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cfg.RuleFiles).Should(BeEmpty())
		g.Expect(cfg.Others).Should(BeEmpty())
	})

	t.Run("invalid content", func(t *testing.T) {
		g := NewWithT(t)

		cfg := PrometheusConfig{}
		err := resources.ExtractContent(
			&corev1.ConfigMap{
				Data: map[string]string{prometheusConfigurationEntry: "{invalid: yaml: data"},
			},
			prometheusConfigurationEntry,
			&cfg,
		)

		g.Expect(err).Should(HaveOccurred())
		g.Expect(cfg.RuleFiles).Should(BeEmpty())
	})
}

func TestSetContent(t *testing.T) {
	cfg := PrometheusConfig{
		RuleFiles: []string{"rule1.yml", "rule2.yml"},
		Others:    map[string]any{"extra": "data"},
	}

	t.Run("Should set content on an empty object", func(t *testing.T) {
		g := NewWithT(t)

		obj := &corev1.ConfigMap{}

		err := resources.SetContent(obj, prometheusConfigurationEntry, cfg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(obj.Data).Should(HaveKey(prometheusConfigurationEntry))

		var storedContent PrometheusConfig
		err = yaml.Unmarshal([]byte(obj.Data[prometheusConfigurationEntry]), &storedContent)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(storedContent.RuleFiles).Should(ConsistOf("rule1.yml", "rule2.yml"))
		g.Expect(storedContent.Others).Should(HaveKeyWithValue("extra", "data"))
	})

	t.Run("Should override existing content", func(t *testing.T) {
		g := NewWithT(t)

		existingObj := &corev1.ConfigMap{
			Data: map[string]string{
				prometheusConfigurationEntry: "rule_files: [old_rule.yml]",
			},
		}

		err := resources.SetContent(existingObj, prometheusConfigurationEntry, cfg)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(existingObj.Data).Should(HaveKey(prometheusConfigurationEntry))

		var storedContent PrometheusConfig
		err = yaml.Unmarshal([]byte(existingObj.Data[prometheusConfigurationEntry]), &storedContent)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(storedContent.RuleFiles).Should(ConsistOf("rule1.yml", "rule2.yml"))
		g.Expect(storedContent.Others).Should(HaveKeyWithValue("extra", "data"))
	})
}
