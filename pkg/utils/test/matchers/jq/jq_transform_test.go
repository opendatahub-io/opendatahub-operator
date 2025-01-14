package jq_test

import (
	"encoding/json"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestExtract(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	g.Expect(`{ "foo": { "a": 1 }}`).Should(
		WithTransform(jq.Extract(`.foo`), WithTransform(json.Marshal,
			jq.Match(`.a == 1`),
		)),
	)

	g.Expect(`{ "status": { "foo": { "bar": "fr", "baz": "fz" } } }`).Should(
		WithTransform(jq.Extract(`.status`),
			And(
				jq.Match(`.foo.bar == "fr"`),
				jq.Match(`.foo.baz == "fz"`),
			),
		),
	)
}

func TestExtractValue(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	transform1 := func(in string) (any, error) {
		return jq.ExtractValue[any](in, `.foo`)
	}

	g.Expect(`{ "foo": { "a": 1 }}`).Should(
		WithTransform(transform1, WithTransform(json.Marshal,
			jq.Match(`.a == 1`),
		)),
	)

	transform2 := func(in string) (any, error) {
		return jq.ExtractValue[any](in, `.status`)
	}

	g.Expect(`{ "status": { "foo": { "bar": "fr", "baz": "fz" } } }`).Should(
		WithTransform(transform2,
			And(
				jq.Match(`.foo.bar == "fr"`),
				jq.Match(`.foo.baz == "fz"`),
			),
		),
	)
}
