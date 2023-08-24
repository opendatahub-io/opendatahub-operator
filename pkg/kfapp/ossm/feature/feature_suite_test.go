package feature_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestOssmFeatures(t *testing.T) {
	RegisterFailHandler(Fail)
	// for integration tests see tests/integration directory
	RunSpecs(t, "Openshift Service Mesh features unit tests")
}
