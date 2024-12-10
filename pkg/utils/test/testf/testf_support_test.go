package testf_test

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestTransform(t *testing.T) {
	g := NewWithT(t)

	t.Run("Change Value of Nested Field", func(t *testing.T) {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind": "Example",
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
		}

		const expression = `.metadata.annotations.key1 |= "new-value"`

		err := testf.Transform(expression)(obj)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(obj.Object).Should(And(
			jq.Match(`.kind == "Example"`),
			jq.Match(`.metadata.annotations.key1 == "new-value"`),
			jq.Match(`.metadata.annotations.key2 == "value2"`),
		))
	})

	t.Run("Invalid JQ Expression", func(t *testing.T) {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind": "Example",
				"data": "value",
			},
		}

		const expression = "~~~invalid-expression"

		err := testf.Transform(expression)(obj)

		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("unable to parse expression"))
	})

	t.Run("Query Result Is Not Map", func(t *testing.T) {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind": "Example",
				"data": []string{"value1", "value2"},
			},
		}

		const expression = ".data"

		err := testf.Transform(expression)(obj)

		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("expected map[string]interface{}"))
	})

	t.Run("Empty Query Result", func(t *testing.T) {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind": "Example",
				"data": map[string]interface{}{
					"name": "value",
				},
			},
		}

		const expression = ".nonexistent"

		err := testf.Transform(expression)(obj)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(obj.Object).Should(And(
			jq.Match(`.kind == "Example"`),
			jq.Match(`.data.name == "value"`),
		))
	})
}

func TestTransformPipeline(t *testing.T) {
	g := NewWithT(t)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "example",
			},
		},
	}

	step1 := func(obj *unstructured.Unstructured) error {
		obj.SetName("transformed-example")
		return nil
	}

	step2 := func(obj *unstructured.Unstructured) error {
		obj.Object["status"] = "active"
		return nil
	}

	step3 := func(obj *unstructured.Unstructured) error {
		if obj.GetName() == "" {
			return errors.New("name cannot be empty")
		}
		return nil
	}

	pipeline := testf.TransformPipeline(step1, step2, step3)

	err := pipeline(obj)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(obj.GetName()).To(Equal("transformed-example"))
	g.Expect(obj.Object["status"]).To(Equal("active"))
}
