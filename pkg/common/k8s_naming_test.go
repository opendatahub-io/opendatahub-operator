package common_test

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ensuring name (e.g. meta.Name) fulfills RFC1123 naming spec", func() {

	type nameConversionCase struct {
		actual   string
		expected string
	}

	DescribeTable("trimming to correct RFC1123 names",
		func(testCase nameConversionCase) {
			Expect(common.TrimToRFC1123Name(testCase.actual)).To(Equal(testCase.expected))
		},
		Entry("empty string should be left unchanged", nameConversionCase{
			actual:   "",
			expected: "",
		}),
		Entry("string longer than 63 characters should be trimmed to 63", nameConversionCase{
			actual:   "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmno",
			expected: "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk",
		}),
		Entry("string with non-alphanumeric characters should have them replaced to hyphens", nameConversionCase{
			actual:   "abc!@#def",
			expected: "abc-def",
		}),
		Entry("string starting with non-alphanumeric character should have it replaced", nameConversionCase{
			actual:   "!abcdef",
			expected: "aabcdef",
		}),
		Entry("string ending with non-alphanumeric character should have it replaced", nameConversionCase{
			actual:   "abcdef!",
			expected: "abcdefz",
		}),
		Entry("string with uppercase characters should be all lowercase", nameConversionCase{
			actual:   "AbCdEf",
			expected: "abcdef",
		}),
		Entry("string with multiple consecutive non-alphanumeric characters should have it folded to one hyphen", nameConversionCase{
			actual:   "abc!!!def",
			expected: "abc-def",
		}),
		Entry("string that has both start and end non-alphanumeric should have them replaced", nameConversionCase{
			actual:   "!abcdef!",
			expected: "aabcdefz",
		}),
		Entry("string that starts and ends with hyphens should have them replaced by alphanumeric characters", nameConversionCase{
			actual:   "-abcdef-",
			expected: "aabcdefz",
		}),
		Entry("string entirely of non-alphanumeric characters should be converted to one letter", nameConversionCase{
			actual:   "!@#$%^&*()",
			expected: "a",
		}),
	)
})
