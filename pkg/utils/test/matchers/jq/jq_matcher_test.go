package jq_test

import (
	"encoding/json"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestMatcher(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	g.Expect(`{"a":1}`).Should(
		jq.Match(`.a == 1`),
	)

	g.Expect(`{"a":1}`).Should(
		Not(
			jq.Match(`.a == 2`),
		),
	)

	g.Expect(`{"Values":[ "foo" ]}`).Should(
		jq.Match(`.Values | if . then any(. == "foo") else false end`),
	)

	g.Expect(`{"Values":[ "foo" ]}`).Should(
		Not(
			jq.Match(`.Values | if . then any(. == "bar") else false end`),
		),
	)

	g.Expect(`{"Values": null}`).Should(
		Not(
			jq.Match(`.Values | if . then any(. == "foo") else false end`),
		),
	)

	g.Expect(`{ "status": { "foo": { "bar": "fr", "baz": "fb" } } }`).Should(
		And(
			jq.Match(`.status.foo.bar == "fr"`),
			jq.Match(`.status.foo.baz == "fb"`),
		),
	)
}

func TestMatcherWithType(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	g.Expect(map[string]any{"a": 1}).
		Should(
			WithTransform(json.Marshal, jq.Match(`.a == 1`)),
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
			WithTransform(json.Marshal, And(
				jq.Match(`.status.foo.bar == "fr"`),
				jq.Match(`.status.foo.baz == "fb"`),
			)),
		)

	g.Expect(map[string]any{"a": 1}).
		Should(jq.Match(`.a == 1`))

	g.Expect(
		struct {
			A int `json:"a"`
		}{
			A: 1,
		}).
		Should(
			WithTransform(json.Marshal, jq.Match(`.a == 1`)),
		)
}
