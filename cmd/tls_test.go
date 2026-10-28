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
	"context"
	"crypto/tls"
	"errors"
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	commonTLS "github.com/openshift/controller-runtime-common/pkg/tls"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
		name           string
		profile        configv1.TLSProfileSpec
		adherence      configv1.TLSAdherencePolicy
		wantError      bool
		wantMin        uint16
		wantCiphers    bool
		wantCipherList []uint16
	}{
		{
			name:           "legacy uses hardened defaults",
			profile:        configv1.TLSProfileSpec{},
			adherence:      configv1.TLSAdherencePolicyNoOpinion,
			wantMin:        tls.VersionTLS12,
			wantCiphers:    true,
			wantCipherList: intermediateCiphers,
		},
		{
			name:           "legacy-adhering uses hardened defaults",
			profile:        configv1.TLSProfileSpec{},
			adherence:      configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			wantMin:        tls.VersionTLS12,
			wantCiphers:    true,
			wantCipherList: intermediateCiphers,
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
			name: "strict honors the Old profile minimum",
			profile: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS10,
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			},
			adherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			wantMin:     tls.VersionTLS10,
			wantCiphers: true,
		},
		{
			name: "strict rejects all unsupported ciphers",
			profile: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"NOT-A-CIPHER", "ALSO-NOT-A-CIPHER"},
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
			if tt.wantCipherList != nil && !reflect.DeepEqual(cfg.CipherSuites, tt.wantCipherList) {
				t.Errorf("CipherSuites = %v, want %v", cfg.CipherSuites, tt.wantCipherList)
			}
			if stringSliceEqual(cfg.NextProtos, []string{"h2", "http/1.1"}) == false {
				t.Errorf("NextProtos = %v", cfg.NextProtos)
			}
		})
	}
}

func TestTLSProfileSchemeRegistered(t *testing.T) {
	_, err := scheme.New(configv1.SchemeGroupVersion.WithKind("APIServer"))
	if err != nil {
		t.Fatalf("APIServer is not registered in the startup scheme: %v", err)
	}
}

func TestFetchTLSProfileWithClientProfiles(t *testing.T) {
	tests := []struct {
		name        string
		profile     *configv1.TLSSecurityProfile
		wantError   bool
		wantMin     uint16
		wantCiphers bool
	}{
		{
			name:        "Old profile",
			profile:     &configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType},
			wantMin:     tls.VersionTLS10,
			wantCiphers: true,
		},
		{
			name: "Custom profile",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
				}},
			},
			wantMin:     tls.VersionTLS12,
			wantCiphers: true,
		},
		{
			name:        "nil profile uses Intermediate",
			wantMin:     tls.VersionTLS12,
			wantCiphers: true,
		},
		{
			name: "Custom profile without spec fails closed",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
			wantError: true,
		},
		{
			name: "unknown profile fails closed",
			profile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileType("FutureProfile"),
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profileClient := newTLSProfileClient(t, tt.profile, configv1.TLSAdherencePolicyStrictAllComponents)
			opts, _, _, watchable, err := fetchTLSProfileWithClient(context.Background(), profileClient)
			if tt.wantError {
				if err == nil {
					t.Fatal("fetchTLSProfileWithClient() expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchTLSProfileWithClient() error = %v", err)
			}
			if !watchable {
				t.Fatal("watchable = false, want true")
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
		})
	}
}

func TestFetchTLSProfileWithClientDeniedReadFailsClosed(t *testing.T) {
	denied := k8serr.NewForbidden(
		schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"},
		commonTLS.APIServerName,
		errors.New("access denied"),
	)
	opts, _, _, watchable, err := fetchTLSProfileWithClient(context.Background(), tlsProfileReadErrorClient{err: denied})
	if err == nil {
		t.Fatal("fetchTLSProfileWithClient() expected a denied-read error")
	}
	if !k8serr.IsForbidden(err) {
		t.Fatalf("fetchTLSProfileWithClient() error = %v, want Forbidden", err)
	}
	if len(opts) != 0 || watchable {
		t.Fatalf("denied read returned fallback options/watchable=%t, want no options and false", watchable)
	}
}

func TestFetchTLSProfileWithClientUnregisteredSchemeFailsClosed(t *testing.T) {
	profileClient := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	opts, _, _, watchable, err := fetchTLSProfileWithClient(context.Background(), profileClient)
	if err == nil {
		t.Fatal("fetchTLSProfileWithClient() expected an unregistered-scheme error")
	}
	if len(opts) != 0 {
		t.Fatalf("unregistered scheme returned %d TLS options, want none", len(opts))
	}
	if watchable {
		t.Fatal("unregistered scheme returned watchable=true, want false")
	}
}

func newTLSProfileClient(t *testing.T, profile *configv1.TLSSecurityProfile, adherence configv1.TLSAdherencePolicy) client.Client {
	t.Helper()

	clientScheme := runtime.NewScheme()
	if err := configv1.Install(clientScheme); err != nil {
		t.Fatalf("register config/v1 scheme: %v", err)
	}
	apiServer := &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: commonTLS.APIServerName},
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: profile,
			TLSAdherence:       adherence,
		},
	}
	return fake.NewClientBuilder().WithScheme(clientScheme).WithObjects(apiServer).Build()
}

type tlsProfileReadErrorClient struct {
	client.Client

	err error
}

func (c tlsProfileReadErrorClient) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return c.err
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
