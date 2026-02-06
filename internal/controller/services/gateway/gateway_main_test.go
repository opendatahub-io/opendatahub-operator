//go:build integration

/*
Copyright 2025.

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

package gateway_test

import (
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

// TestMain sets up the shared test environments for all integration tests.
// This dramatically speeds up tests as envtest only starts once per auth mode.
func TestMain(m *testing.M) {
	// Setup shared OAuth test environment
	OAuthTestEnv = SetupTestEnvForMain(string(configv1.AuthenticationTypeIntegratedOAuth), "apps.oauth-test.example.com")

	// Setup shared OIDC test environment
	OIDCTestEnv = SetupTestEnvForMain("OIDC", "apps.oidc-test.example.com")

	// Run all tests
	code := m.Run()

	// Cleanup OAuth environment
	if OAuthTestEnv != nil {
		OAuthTestEnv.Cancel()
		OAuthTestEnv.TestEnv.Stop() //nolint:errcheck
	}

	// Cleanup OIDC environment
	if OIDCTestEnv != nil {
		OIDCTestEnv.Cancel()
		OIDCTestEnv.TestEnv.Stop() //nolint:errcheck
	}

	os.Exit(code)
}
