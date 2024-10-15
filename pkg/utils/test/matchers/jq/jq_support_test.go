//nolint:testpackage
package jq

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/onsi/gomega/gbytes"

	. "github.com/onsi/gomega"
)

func TestToType(t *testing.T) {
	t.Parallel()

	typeTestData := []byte(`{ "foo": "bar" }`)
	g := NewWithT(t)

	items := map[string]func() any{
		"gbytes": func() any {
			b := gbytes.NewBuffer()

			_, err := b.Write(typeTestData)
			g.Expect(err).ShouldNot(HaveOccurred())

			return b
		},
		"bytes": func() any {
			return typeTestData
		},
		"string": func() any {
			return string(typeTestData)
		},
		"raw-message": func() any {
			return json.RawMessage(typeTestData)
		},
	}

	for name, fn := range items {
		f := fn

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tt, err := toType(f())

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(tt).Should(Satisfy(func(in any) bool {
				return reflect.TypeOf(in).Kind() == reflect.Map
			}))
		})
	}
}
