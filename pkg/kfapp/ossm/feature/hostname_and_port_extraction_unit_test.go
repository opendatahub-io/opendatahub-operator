package feature_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/ossm/feature"
)

var _ = Describe("extracting hostname and port from URL", func() {

	It("should extract hostname and port for HTTP URL", func() {
		hostname, port, err := feature.ExtractHostNameAndPort("http://opendatahub.io:8080/path")
		Expect(err).ToNot(HaveOccurred())
		Expect(hostname).To(Equal("opendatahub.io"))
		Expect(port).To(Equal("8080"))
	})

	It("should return original URL if it does not start with http(s) but with other valid protocol", func() {
		originalURL := "gopher://opendatahub.io"
		hostname, port, err := feature.ExtractHostNameAndPort(originalURL)
		Expect(err).ToNot(HaveOccurred())
		Expect(hostname).To(Equal(originalURL))
		Expect(port).To(Equal(""))
	})

	It("should handle invalid URLs by returning corresponding error", func() {
		invalidURL := ":opendatahub.io"
		_, _, err := feature.ExtractHostNameAndPort(invalidURL)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("missing protocol scheme")))
	})

	It("should handle URLs without port and default to 443 for HTTPS", func() {
		hostname, port, err := feature.ExtractHostNameAndPort("https://opendatahub.io")
		Expect(err).ToNot(HaveOccurred())
		Expect(hostname).To(Equal("opendatahub.io"))
		Expect(port).To(Equal("443"))
	})

	It("should handle URLs without port and default to 80 for HTTP", func() {
		hostname, port, err := feature.ExtractHostNameAndPort("http://opendatahub.io")
		Expect(err).ToNot(HaveOccurred())
		Expect(hostname).To(Equal("opendatahub.io"))
		Expect(port).To(Equal("80"))
	})
})
