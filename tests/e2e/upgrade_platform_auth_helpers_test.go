package e2e_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// transportTLSArgs is the set of transport-level TLS argument flags whose
// values are unordered lists allowed to evolve between versions (cipher
// suite and curve hardening); authentication-relevant args must not change
// across an upgrade.
var transportTLSArgs = map[string]struct{}{
	"--tls-cipher-suite":      {},
	"--tls-curve-preferences": {},
}

// secretLikeArgFlags are argument flags that may carry credential material.
// argDigestEntries refuses to process any arg starting with one of these:
// secret-bearing args must never enter the upgrade baseline, not even as a
// digest (a low-entropy secret could be brute-forced from it).
var secretLikeArgFlags = []string{"--cookie-secret", "--client-secret"}

// sha256Hex returns the hex-encoded SHA-256 digest of value. The value itself
// is never stored or logged; only the digest is.
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// secretDataDigests returns the per-key SHA-256 digest of a Secret's data
// values. Values are digested exactly as stored in secret.Data; only digests
// are returned, never raw values, so a failing comparison cannot leak secret
// material into test output (CWE-532).
func secretDataDigests(secret *corev1.Secret) map[string]string {
	digests := make(map[string]string, len(secret.Data))
	for key, value := range secret.Data {
		digests[key] = sha256Hex(string(value))
	}
	return digests
}

// isTransportTLSArg reports whether flag is in the transport-TLS exemption
// set: exact set membership only. The two members carry unordered cipher and
// curve lists that may evolve between versions; every other arg — including
// other --tls-* args such as --tls-cert-file or --tls-min-version — must not
// change across an upgrade, so it is compared, never dropped.
func isTransportTLSArg(flag string) bool {
	_, ok := transportTLSArgs[flag]
	return ok
}

// normalizedArg normalizes a container argument for stable comparison: the
// flag name is lowercased and split from its value on the first "=". Values
// are returned verbatim. Transport/TLS args are dropped (ok=false) — they are
// allowed to evolve between versions, while authentication-relevant args are
// not. Stripping the value of secret-like flags is deliberately NOT done
// here; argDigestEntries refuses such args outright.
func normalizedArg(arg string) (string, bool) {
	flag, value, hasValue := strings.Cut(arg, "=")
	flag = strings.ToLower(flag)
	if isTransportTLSArg(flag) {
		return "", false
	}
	if hasValue {
		return flag + "=" + value, true
	}
	return flag, true
}

// argDigestEntries maps each non-transport container arg to a SHA-256 digest
// of its value, keyed by normalized flag name. A flag occurring more than
// once digests the sorted multiset of its values joined with a NUL
// separator, so dropping or adding one occurrence of a multi-value flag —
// e.g. --skip-auth-route, whose values carve auth-bypass paths — changes
// the entry, while occurrence order alone does not. A flag occurring once
// digests its value alone, keeping baselines recorded by earlier suite
// versions comparable. Values are hashed, never stored or logged in clear
// text, so an arg that evolves to carry credentials cannot leak through the
// baseline ConfigMap or failure messages. It returns an error if any arg
// starts with a secret-like flag (--cookie-secret, --client-secret):
// secret-bearing args are refused, not fingerprinted. The error names the
// flag only, never the value.
func argDigestEntries(args []string) (map[string]string, error) {
	for _, arg := range args {
		lowerArg := strings.ToLower(arg)
		for _, secretFlag := range secretLikeArgFlags {
			if strings.HasPrefix(lowerArg, secretFlag) {
				return nil, fmt.Errorf(
					"refusing to digest secret-like arg flag %q: secret-bearing args must not enter the upgrade baseline",
					secretFlag,
				)
			}
		}
	}

	valuesByFlag := make(map[string][]string, len(args))
	for _, arg := range args {
		normalized, ok := normalizedArg(arg)
		if !ok {
			continue
		}
		flag, value, _ := strings.Cut(normalized, "=")
		valuesByFlag[flag] = append(valuesByFlag[flag], value)
	}

	entries := make(map[string]string, len(valuesByFlag))
	for flag, values := range valuesByFlag {
		if len(values) == 1 {
			entries[flag] = sha256Hex(values[0])
			continue
		}
		sorted := slices.Clone(values)
		slices.Sort(sorted)
		entries[flag] = sha256Hex(strings.Join(sorted, "\x00"))
	}
	return entries, nil
}

// TestSha256Hex pins sha256Hex against known SHA-256 vectors so the digest
// helpers cannot silently drift: upgrade baselines recorded by these helpers
// must stay comparable across suite versions.
func TestSha256Hex(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "known vector abc",
			value: "abc",
			want:  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			name:  "empty string",
			value: "",
			want:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "oauth2 proxy cookie name",
			value: "_oauth2_proxy",
			want:  "f741b64673dd2d1ea4fc98e2417ecd59556bfa00f0b3c2b88b1257768765e3b5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, sha256Hex(tt.value))
		})
	}
}

// TestNormalizedArgDropsTransport proves exactly the transport-TLS exemption
// set is dropped from the baseline (ok=false): cipher-suite and
// curve-preference values are unordered lists allowed to evolve between
// versions (hardening). Every other arg — including near-miss --tls-* args
// like --tls-cert-file or --tls-min-version — is compared (ok=true), because
// authentication-relevant args must not change across an upgrade.
func TestNormalizedArgDropsTransport(t *testing.T) {
	tests := []struct {
		name   string
		arg    string
		want   string
		wantOK bool
	}{
		{
			name:   "tls cipher suite is exempt",
			arg:    "--tls-cipher-suite=TLS_AES_128_GCM_SHA256",
			wantOK: false,
		},
		{
			name:   "tls curve preferences is exempt",
			arg:    "--tls-curve-preferences=X25519:P256",
			wantOK: false,
		},
		{
			name:   "near-miss tls cert file is compared, not dropped",
			arg:    "--tls-cert-file=/x",
			want:   "--tls-cert-file=/x",
			wantOK: true,
		},
		{
			name:   "near-miss tls key file is compared, not dropped",
			arg:    "--tls-key-file=/etc/tls-certs/tls.key",
			want:   "--tls-key-file=/etc/tls-certs/tls.key",
			wantOK: true,
		},
		{
			name:   "near-miss tls min version is compared, not dropped",
			arg:    "--tls-min-version=TLS1.2",
			want:   "--tls-min-version=TLS1.2",
			wantOK: true,
		},
		{
			name:   "auth-relevant arg is kept with value intact",
			arg:    "--cookie-name=_oauth2_proxy",
			want:   "--cookie-name=_oauth2_proxy",
			wantOK: true,
		},
		{
			name:   "flag name is lowercased, value case is preserved",
			arg:    "--Cookie-Name=_OAuth2_Proxy",
			want:   "--cookie-name=_OAuth2_Proxy",
			wantOK: true,
		},
		{
			name:   "bare flag without a value is kept",
			arg:    "--pass-user-headers",
			want:   "--pass-user-headers",
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizedArg(tt.arg)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestArgDigestEntriesLeakRefusal pins the leak guard: an arg starting with
// a secret-like flag must be refused outright instead of fingerprinted, and
// the error must name only the flag — never the arg's value, which could
// carry credential material.
func TestArgDigestEntriesLeakRefusal(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "cookie secret value",
			args: []string{"--cookie-secret=xyz"},
		},
		{
			name: "client secret value",
			args: []string{"--client-secret=xyz"},
		},
		{
			name: "mixed with regular args",
			args: []string{"--upstream=http://x", "--cookie-secret=xyz"},
		},
		{
			name: "guard is case-insensitive",
			args: []string{"--Cookie-Secret=xyz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := argDigestEntries(tt.args)
			require.Error(t, err)
			assert.Nil(t, entries)
			assert.Contains(t, err.Error(), "secret")
			assert.NotContains(t, err.Error(), "xyz")
		})
	}
}

// TestArgDigestEntriesDigests proves non-transport args map to flag-keyed
// SHA-256 entries of their values, so only digests — never values — enter
// the upgrade baseline.
func TestArgDigestEntriesDigests(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{
			name: "auth args digest to flag-keyed entries",
			args: []string{"--cookie-name=_oauth2_proxy", "--upstream=http://x"},
			want: map[string]string{
				"--cookie-name": "f741b64673dd2d1ea4fc98e2417ecd59556bfa00f0b3c2b88b1257768765e3b5",
				"--upstream":    "037ab55168fe8769f2a4c041c2ea5d8b3112e28b83c9ca8cf7d35445d7a03cbe",
			},
		},
		{
			name: "flag names are lowercased",
			args: []string{"--Skip-Auth-Route=/x"},
			want: map[string]string{
				"--skip-auth-route": "b3d1db318671a024a7e4b433389f8820d6ca466e2cf700afc29f37ed64f2fa0d",
			},
		},
		{
			name: "bare flag digests the empty value",
			args: []string{"--pass-user-headers"},
			want: map[string]string{
				"--pass-user-headers": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "exempt-set TLS args are excluded from entries",
			args: []string{"--tls-cipher-suite=TLS_AES_128_GCM_SHA256", "--cookie-name=_oauth2_proxy"},
			want: map[string]string{
				"--cookie-name": "f741b64673dd2d1ea4fc98e2417ecd59556bfa00f0b3c2b88b1257768765e3b5",
			},
		},
		{
			name: "near-miss tls args are compared, not excluded",
			args: []string{"--tls-cert-file=/x", "--cookie-name=_oauth2_proxy"},
			want: map[string]string{
				"--tls-cert-file": "b3d1db318671a024a7e4b433389f8820d6ca466e2cf700afc29f37ed64f2fa0d",
				"--cookie-name":   "f741b64673dd2d1ea4fc98e2417ecd59556bfa00f0b3c2b88b1257768765e3b5",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := argDigestEntries(tt.args)
			require.NoError(t, err)
			require.Equal(t, tt.want, entries)
		})
	}
}

// TestArgDigestEntriesRepeatedFlags pins that every occurrence of a repeated
// flag enters the comparison: the entry digest covers the sorted multiset of
// the flag's values, joined with a NUL separator. Last-write-wins digests
// would let an upgrade silently drop one occurrence of a multi-value flag
// such as --skip-auth-route, whose values carve auth-bypass paths.
func TestArgDigestEntriesRepeatedFlags(t *testing.T) {
	baseline, err := argDigestEntries([]string{"--skip-auth-route=/a", "--skip-auth-route=/b"})
	require.NoError(t, err)

	// Occurrence order alone must not register as a change.
	reordered, err := argDigestEntries([]string{"--skip-auth-route=/b", "--skip-auth-route=/a"})
	require.NoError(t, err)
	require.Equal(t, baseline, reordered, "reordering occurrences of a repeated flag must not change the digest")

	// Dropping one occurrence must register: with last-write-wins digests,
	// an upgrade dropping the first of two values is invisible, because
	// both sides digest only the surviving last value.
	droppedFirst, err := argDigestEntries([]string{"--skip-auth-route=/b"})
	require.NoError(t, err)
	require.NotEqual(t, baseline["--skip-auth-route"], droppedFirst["--skip-auth-route"],
		"dropping one occurrence of a repeated flag must change the digest")

	// The digest is over the sorted values joined with a NUL separator.
	require.Equal(t, sha256Hex("/a\x00/b"), baseline["--skip-auth-route"])

	// Baseline compatibility: a flag occurring once digests its value
	// alone — no separator, no list framing — so baselines recorded before
	// repeated-flag support remain comparable for flags that never repeat.
	single, err := argDigestEntries([]string{"--cookie-name=_oauth2_proxy"})
	require.NoError(t, err)
	require.Equal(t, "f741b64673dd2d1ea4fc98e2417ecd59556bfa00f0b3c2b88b1257768765e3b5",
		single["--cookie-name"])
}

// TestSecretDataDigests verifies per-key digests of a Secret's data values
// and pins that raw values never appear in the output — only digests, so a
// failing comparison cannot leak secret material into test logs (CWE-532).
func TestSecretDataDigests(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_COOKIE_SECRET": []byte("super-secret-cookie-value-1"),
			"OAUTH2_PROXY_CLIENT_ID":     []byte("data-science"),
		},
	}

	digests := secretDataDigests(secret)
	require.Len(t, digests, 2)
	require.Equal(
		t,
		"25ecccc2e8e103558655888c5ba838c277c52905730912c60b07724976748bf0",
		digests["OAUTH2_PROXY_COOKIE_SECRET"],
	)
	require.Equal(
		t,
		"970677b8156d0aadee611d19d0e8cbecd924a4ecc7af70d163c9bbcab9676d71",
		digests["OAUTH2_PROXY_CLIENT_ID"],
	)

	// Leak guard: the rendered output must contain digests only.
	rendered := fmt.Sprintf("%v", digests)
	assert.NotContains(t, rendered, "super-secret-cookie-value-1")
	assert.NotContains(t, rendered, "data-science")

	t.Run("secret with no data yields no digests", func(t *testing.T) {
		assert.Empty(t, secretDataDigests(&corev1.Secret{}))
	})
}
