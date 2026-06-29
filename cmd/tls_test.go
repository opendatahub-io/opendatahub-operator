/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"

	. "github.com/onsi/gomega"
)

func TestIntermediateCiphersAreValid(t *testing.T) {
	g := NewWithT(t)
	g.Expect(intermediateCiphers).NotTo(BeEmpty())
	for _, id := range intermediateCiphers {
		g.Expect(tls.CipherSuiteName(id)).NotTo(BeEmpty(), "unknown cipher suite ID %d", id)
	}
}

func TestHardenedDefaultsTLSConfig(t *testing.T) {
	g := NewWithT(t)
	cfg := &tls.Config{} //nolint:gosec
	cfg.MinVersion = tls.VersionTLS12
	cfg.CipherSuites = intermediateCiphers
	cfg.NextProtos = []string{"h2", "http/1.1"}

	g.Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	g.Expect(cfg.CipherSuites).To(Equal(intermediateCiphers))
	g.Expect(cfg.NextProtos).To(Equal([]string{"h2", "http/1.1"}))
}

func TestSetKubeRBACProxyTLSEnv_Intermediate(t *testing.T) {
	t.Setenv(envKubeRBACProxyTLSMinVersion, "")
	t.Setenv(envKubeRBACProxyTLSCipherSuites, "")
	g := NewWithT(t)

	profile := configv1.TLSProfileSpec{
		MinTLSVersion: "VersionTLS12",
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
			"ECDHE-ECDSA-AES128-GCM-SHA256",
			"ECDHE-RSA-AES128-GCM-SHA256",
			"ECDHE-ECDSA-AES256-GCM-SHA384",
			"ECDHE-RSA-AES256-GCM-SHA384",
			"ECDHE-ECDSA-CHACHA20-POLY1305",
			"ECDHE-RSA-CHACHA20-POLY1305",
		},
	}
	setKubeRBACProxyTLSEnv(profile, true)

	g.Expect(os.Getenv(envKubeRBACProxyTLSMinVersion)).To(Equal("VersionTLS12"))
	suites := os.Getenv(envKubeRBACProxyTLSCipherSuites)
	g.Expect(suites).NotTo(BeEmpty())
	g.Expect(suites).To(ContainSubstring("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"))
	g.Expect(suites).To(ContainSubstring("TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"))
}

func TestSetKubeRBACProxyTLSEnv_Modern(t *testing.T) {
	t.Setenv(envKubeRBACProxyTLSMinVersion, "")
	t.Setenv(envKubeRBACProxyTLSCipherSuites, "")
	g := NewWithT(t)

	profile := configv1.TLSProfileSpec{
		MinTLSVersion: "VersionTLS13",
		Ciphers:       []string{},
	}
	setKubeRBACProxyTLSEnv(profile, true)

	g.Expect(os.Getenv(envKubeRBACProxyTLSMinVersion)).To(Equal("VersionTLS13"))
	g.Expect(os.Getenv(envKubeRBACProxyTLSCipherSuites)).To(BeEmpty())
}

func TestSetKubeRBACProxyTLSEnv_Old(t *testing.T) {
	t.Setenv(envKubeRBACProxyTLSMinVersion, "")
	t.Setenv(envKubeRBACProxyTLSCipherSuites, "")
	g := NewWithT(t)

	profile := configv1.TLSProfileSpec{
		MinTLSVersion: "VersionTLS10",
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
			"ECDHE-ECDSA-AES128-GCM-SHA256",
			"ECDHE-RSA-AES128-GCM-SHA256",
			"ECDHE-ECDSA-AES256-GCM-SHA384",
			"ECDHE-RSA-AES256-GCM-SHA384",
			"ECDHE-ECDSA-CHACHA20-POLY1305",
			"ECDHE-RSA-CHACHA20-POLY1305",
			"ECDHE-ECDSA-AES128-SHA256",
			"ECDHE-RSA-AES128-SHA256",
			"ECDHE-ECDSA-AES128-SHA",
			"ECDHE-RSA-AES128-SHA",
			"AES128-GCM-SHA256",
			"AES256-GCM-SHA384",
			"AES128-SHA256",
			"AES128-SHA",
			"DES-CBC3-SHA",
		},
	}
	setKubeRBACProxyTLSEnv(profile, true)

	g.Expect(os.Getenv(envKubeRBACProxyTLSMinVersion)).To(Equal("VersionTLS12"))
	suites := os.Getenv(envKubeRBACProxyTLSCipherSuites)
	g.Expect(suites).NotTo(BeEmpty())
	g.Expect(suites).To(ContainSubstring("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"))
}

func TestSetKubeRBACProxyTLSEnv_CustomEmptyMinVersion(t *testing.T) {
	t.Setenv(envKubeRBACProxyTLSMinVersion, "")
	t.Setenv(envKubeRBACProxyTLSCipherSuites, "")
	g := NewWithT(t)

	profile := configv1.TLSProfileSpec{
		Ciphers: []string{
			"ECDHE-ECDSA-AES128-GCM-SHA256",
			"ECDHE-RSA-AES128-GCM-SHA256",
		},
	}
	setKubeRBACProxyTLSEnv(profile, true)

	g.Expect(os.Getenv(envKubeRBACProxyTLSMinVersion)).To(Equal("VersionTLS12"))
	suites := os.Getenv(envKubeRBACProxyTLSCipherSuites)
	g.Expect(suites).NotTo(BeEmpty())
	g.Expect(suites).To(ContainSubstring("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"))
}

func TestSetKubeRBACProxyTLSEnv_InvalidMinVersion(t *testing.T) {
	t.Setenv(envKubeRBACProxyTLSMinVersion, "")
	t.Setenv(envKubeRBACProxyTLSCipherSuites, "")
	g := NewWithT(t)

	profile := configv1.TLSProfileSpec{
		MinTLSVersion: "SomeBogusVersion",
		Ciphers: []string{
			"ECDHE-RSA-AES128-GCM-SHA256",
		},
	}
	setKubeRBACProxyTLSEnv(profile, true)

	g.Expect(os.Getenv(envKubeRBACProxyTLSMinVersion)).To(Equal("VersionTLS12"))
	suites := os.Getenv(envKubeRBACProxyTLSCipherSuites)
	g.Expect(suites).NotTo(BeEmpty())
	g.Expect(suites).To(ContainSubstring("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"))
}

func TestSetKubeRBACProxyTLSEnv_NonOpenShift(t *testing.T) {
	t.Setenv(envKubeRBACProxyTLSMinVersion, "")
	t.Setenv(envKubeRBACProxyTLSCipherSuites, "")
	g := NewWithT(t)

	setKubeRBACProxyTLSEnv(configv1.TLSProfileSpec{}, false)

	g.Expect(os.Getenv(envKubeRBACProxyTLSMinVersion)).To(Equal("VersionTLS12"))
	suites := os.Getenv(envKubeRBACProxyTLSCipherSuites)
	g.Expect(suites).NotTo(BeEmpty())
	for _, id := range intermediateCiphers {
		g.Expect(suites).To(ContainSubstring(tls.CipherSuiteName(id)))
	}
}
