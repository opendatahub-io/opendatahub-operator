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

package gateway

import (
	"context"

	pkgtls "github.com/opendatahub-io/odh-platform-utilities/framework/tls"
	configv1 "github.com/openshift/api/config/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubeAuthProxyTLSFromProfile resolves a TLSSecurityProfile to version, cipher, and curve strings
// in the short format ("TLS1.2") used by kube-auth-proxy.
func KubeAuthProxyTLSFromProfile(ctx context.Context, profile *configv1.TLSSecurityProfile) (string, string, string, error) {
	return pkgtls.FromProfileWithCurvePreferences(ctx, profile, pkgtls.FormatShort)
}

// GetKubeAuthProxyTLSFromAPIServer fetches the cluster TLS profile and returns version, cipher, and curve strings
// in the short format ("TLS1.2") used by kube-auth-proxy.
func GetKubeAuthProxyTLSFromAPIServer(ctx context.Context, cli client.Reader) (string, string, string, error) {
	return pkgtls.FromAPIServerWithCurvePreferences(ctx, cli, pkgtls.FormatShort)
}
