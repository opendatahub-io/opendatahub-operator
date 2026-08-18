package v1alpha1

import (
	"regexp"
	"testing"

	. "github.com/onsi/gomega"
)

// issuerURLPattern is the regex from the kubebuilder validation annotation on OIDCConfig.IssuerURL.
// Duplicated here so the unit test is an independent oracle — if someone accidentally relaxes
// the CRD pattern, this test still passes; the envtest admission test catches the drift.
const issuerURLPattern = `^https://(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?\.)*[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?::(?:6553[0-5]|655[0-2][0-9]|65[0-4][0-9]{2}|6[0-4][0-9]{3}|[1-5][0-9]{4}|[1-9][0-9]{0,3}))?(?:/[^?#]*)?$`

func TestIssuerURLPatternRejectsNonHTTPS(t *testing.T) {
	g := NewWithT(t)
	re := regexp.MustCompile(issuerURLPattern)

	cases := []struct {
		name string
		url  string
	}{
		{name: "http scheme", url: "http://example.com"},
		{name: "ftp scheme", url: "ftp://example.com"},
		{name: "no scheme", url: "example.com"},
		{name: "uppercase HTTP scheme", url: "HTTP://example.com"},
		{name: "uppercase HTTPS scheme", url: "HTTPS://example.com"},
		{name: "empty string", url: ""},
		{name: "scheme only", url: "https://"},
		{name: "missing double slash", url: "https:example.com"},
		{name: "single slash", url: "https:/example.com"},
		{name: "javascript scheme", url: "javascript://example.com"},
		{name: "data URI", url: "data:text/html,<h1>hi</h1>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g.Expect(re.MatchString(tc.url)).To(BeFalse(),
				"expected pattern to reject non-HTTPS URL %q", tc.url)
		})
	}
}

func TestIssuerURLPatternRejectsSpecialCharacters(t *testing.T) {
	g := NewWithT(t)
	re := regexp.MustCompile(issuerURLPattern)

	cases := []struct {
		name string
		url  string
	}{
		{name: "semicolon in hostname (shell injection)", url: "https://host;echo pwned.com"},
		{name: "dollar command substitution", url: "https://host$(cmd).com"},
		{name: "backtick command substitution", url: "https://host`id`.com"},
		{name: "Go template braces", url: "https://host{{.Value}}.com"},
		{name: "angle brackets", url: "https://host<script>.com"},
		{name: "double quotes in hostname", url: `https://host"quoted".com`},
		{name: "single quotes in hostname", url: "https://host'quoted'.com"},
		{name: "space in hostname", url: "https://host name.com"},
		{name: "newline in hostname", url: "https://host\nname.com"},
		{name: "tab in hostname", url: "https://host\tname.com"},
		{name: "pipe in hostname", url: "https://host|cat.com"},
		{name: "ampersand in hostname", url: "https://host&cmd.com"},
		{name: "at sign in hostname", url: "https://user@host.com"},
		{name: "hash in hostname", url: "https://host#fragment.com"},
		{name: "percent encoding in hostname", url: "https://host%00.com"},
		{name: "backslash in hostname", url: `https://host\name.com`},
		{name: "underscore in hostname", url: "https://host_name.com"},
		{name: "hostname starts with hyphen", url: "https://-hostname.com"},
		{name: "hostname starts with dot", url: "https://.hostname.com"},
		{name: "consecutive dots in hostname", url: "https://a..b.com"},
		{name: "trailing dot in hostname", url: "https://example.com.."},
		{name: "many consecutive dots in hostname", url: "https://a...........com"},
		{name: "port above valid range", url: "https://example.com:65536"},
		{name: "port far above valid range", url: "https://example.com:99999999"},
		{name: "port zero", url: "https://example.com:0"},
		{name: "query string in path", url: "https://example.com/realm?foo=bar"},
		{name: "query string after trailing slash", url: "https://example.com/?foo=bar"},
		{name: "bare query string", url: "https://example.com?foo=bar"},
		{name: "fragment in path", url: "https://example.com/realm#section"},
		{name: "bare fragment", url: "https://example.com#section"},
		{name: "query and fragment in path", url: "https://example.com/realm?foo=bar#section"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g.Expect(re.MatchString(tc.url)).To(BeFalse(),
				"expected pattern to reject URL with special characters %q", tc.url)
		})
	}
}

func TestIssuerURLPatternAcceptsValidURLs(t *testing.T) {
	g := NewWithT(t)
	re := regexp.MustCompile(issuerURLPattern)

	cases := []struct {
		name string
		url  string
	}{
		{name: "typical keycloak URL", url: "https://keycloak.example.com/realms/myorg"},
		{name: "google accounts", url: "https://accounts.google.com"},
		{name: "with port", url: "https://auth.example.com:8443/realms/test"},
		{name: "with non-standard port", url: "https://localhost:9443"},
		{name: "minimal valid hostname", url: "https://a.b"},
		{name: "IP-like hostname", url: "https://192.168.1.1"},
		{name: "IP with port", url: "https://10.0.0.1:6443"},
		{name: "trailing slash", url: "https://example.com/"},
		{name: "deep path", url: "https://auth.example.com/a/b/c/d"},
		{name: "hostname with hyphens", url: "https://my-auth-server.example.com"},
		{name: "multiple subdomains", url: "https://a.b.c.d.example.com/realm"},
		{name: "maximum valid port", url: "https://example.com:65535"},
		{name: "minimum valid port", url: "https://example.com:1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g.Expect(re.MatchString(tc.url)).To(BeTrue(),
				"expected pattern to accept valid HTTPS URL %q", tc.url)
		})
	}
}
