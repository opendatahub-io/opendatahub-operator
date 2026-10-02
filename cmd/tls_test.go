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
	"errors"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

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
	cfg := &tls.Config{}
	cfg.MinVersion = tls.VersionTLS12
	cfg.CipherSuites = intermediateCiphers
	cfg.NextProtos = []string{"h2", "http/1.1"}

	g.Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	g.Expect(cfg.CipherSuites).To(Equal(intermediateCiphers))
	g.Expect(cfg.NextProtos).To(Equal([]string{"h2", "http/1.1"}))
}

func TestBuildManagerTLSOpts(t *testing.T) {
	tests := []struct {
		name        string
		profile     configv1.TLSProfileSpec
		adherence   configv1.TLSAdherencePolicy
		wantError   bool
		wantMin     uint16
		wantCiphers bool
	}{
		{
			name:        "legacy uses hardened defaults",
			profile:     configv1.TLSProfileSpec{},
			adherence:   configv1.TLSAdherencePolicyNoOpinion,
			wantMin:     tls.VersionTLS12,
			wantCiphers: true,
		},
		{
			name: "strict applies the profile",
			profile: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
				Groups:        []configv1.TLSGroup{configv1.TLSGroupX25519},
			},
			adherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			wantMin:     tls.VersionTLS12,
			wantCiphers: true,
		},
		{
			name: "unknown adherence is strict",
			profile: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			},
			adherence:   configv1.TLSAdherencePolicy("FuturePolicy"),
			wantMin:     tls.VersionTLS12,
			wantCiphers: true,
		},
		{
			name: "strict rejects unsupported version",
			profile: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS10,
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			},
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			wantError: true,
		},
		{
			name: "strict rejects all unsupported ciphers",
			profile: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"DHE-RSA-AES128-GCM-SHA256", "DHE-RSA-AES256-GCM-SHA384"},
			},
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			wantError: true,
		},
		{
			name: "strict allows TLS 1.3 without cipher restriction",
			profile: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS13,
			},
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			wantMin:   tls.VersionTLS13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, _, err := buildManagerTLSOpts(tt.profile, tt.adherence)
			if tt.wantError {
				requireError(t, err)
				return
			}
			if err != nil {
				t.Fatalf("buildManagerTLSOpts() error = %v", err)
			}
			cfg := &tls.Config{}
			for _, opt := range opts {
				opt(cfg)
			}
			if cfg.MinVersion != tt.wantMin {
				t.Errorf("MinVersion = %d, want %d", cfg.MinVersion, tt.wantMin)
			}
			if tt.wantCiphers && len(cfg.CipherSuites) == 0 {
				t.Error("CipherSuites is empty")
			}
			if stringSliceEqual(cfg.NextProtos, []string{"h2", "http/1.1"}) == false {
				t.Errorf("NextProtos = %v", cfg.NextProtos)
			}
		})
	}
}

func TestClassifyTLSProfileReadError(t *testing.T) {
	noMatch := &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "config.openshift.io", Kind: "APIServer"}}
	tests := []struct {
		name    string
		err     error
		outcome tlsReadOutcome
	}{
		{name: "not found", err: k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster"), outcome: tlsReadFallback},
		{name: "no match", err: noMatch, outcome: tlsReadFallback},
		{name: "service unavailable", err: k8serr.NewServiceUnavailable("unavailable"), outcome: tlsReadTransient},
		{name: "forbidden", err: k8serr.NewForbidden(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster", nil), outcome: tlsReadRefuse},
		{name: "unauthorized", err: k8serr.NewUnauthorized("unauthorized"), outcome: tlsReadRefuse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyTLSProfileReadError(tt.err); got != tt.outcome {
				t.Errorf("classifyTLSProfileReadError() = %v, want %v", got, tt.outcome)
			}
		})
	}
}

func TestClassifyTLSAdherenceReadError(t *testing.T) {
	noMatch := &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "config.openshift.io", Kind: "APIServer"}}
	if got := classifyTLSAdherenceReadError(noMatch); got != tlsReadFallback {
		t.Errorf("NoMatch outcome = %v, want fallback", got)
	}
	if got := classifyTLSAdherenceReadError(k8serr.NewInternalError(errors.New("internal error"))); got != tlsReadTransient {
		t.Errorf("InternalError outcome = %v, want transient", got)
	}
	if got := classifyTLSAdherenceReadError(k8serr.NewUnauthorized("unauthorized")); got != tlsReadRefuse {
		t.Errorf("Unauthorized outcome = %v, want refuse", got)
	}
}

func requireError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
}

func stringSliceEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
