package yq_test

import (
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/yq"

	. "github.com/onsi/gomega"
)

func TestMatcher(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	g.Expect(`a: 1`).Should(
		yq.Match(`.a == 1`),
	)
	g.Expect(`a: 1`).Should(
		Not(
			yq.Match(`.a == 2`),
		),
	)
}

func TestMatcherWithType(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	g.Expect(map[string]any{"a": 1}).Should(
		WithTransform(yaml.Marshal, yq.Match(`.a == 1`)),
	)

	g.Expect(
		map[string]any{
			"status": map[string]any{
				"foo": map[string]any{
					"bar": "fr",
					"baz": "fb",
				},
			},
		}).
		Should(
			WithTransform(yaml.Marshal, And(
				yq.Match(`.status.foo.bar == "fr"`),
				yq.Match(`.status.foo.baz == "fb"`),
			)),
		)

	g.Expect(
		struct {
			A int `yaml:"a"`
		}{
			A: 1,
		}).
		Should(
			WithTransform(yaml.Marshal, yq.Match(`.a == 1`)),
		)
}
