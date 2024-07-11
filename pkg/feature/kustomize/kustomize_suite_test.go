package kustomize_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKustomizeManifests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kustomize Manifests Suite")
}
