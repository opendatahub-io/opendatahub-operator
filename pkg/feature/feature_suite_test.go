package feature_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFeatures(t *testing.T) {
	RegisterFailHandler(Fail)
	// for integration tests see tests/integration directory
	RunSpecs(t, "Features unit tests")
}
