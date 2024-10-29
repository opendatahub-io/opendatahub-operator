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
