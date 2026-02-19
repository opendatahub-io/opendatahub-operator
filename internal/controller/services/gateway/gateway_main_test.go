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

// TestMain starts shared envtest environments for OAuth and OIDC; all integration tests use these. Cleanup runs after m.Run().
func TestMain(m *testing.M) {
	OAuthTestEnv = SetupTestEnvForMain(string(configv1.AuthenticationTypeIntegratedOAuth), OAuthClusterDomain)
	OIDCTestEnv = SetupTestEnvForMain("OIDC", OIDCClusterDomain)

	// Run all tests
	code := m.Run()

	// Cleanup OAuth environment
	if OAuthTestEnv != nil {
		OAuthTestEnv.Cancel()
		OAuthTestEnv.TestEnv.Stop()
	}

	// Cleanup OIDC environment
	if OIDCTestEnv != nil {
		OIDCTestEnv.Cancel()
		OIDCTestEnv.TestEnv.Stop()
	}

	os.Exit(code)
}
