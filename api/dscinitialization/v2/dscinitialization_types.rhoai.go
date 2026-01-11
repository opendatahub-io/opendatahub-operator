//go:build rhoai

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

package v2

import (
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

// DSCInitializationSpec defines the desired state of DSCInitialization.
type DSCInitializationSpec struct {
	// Namespace for applications to be installed, non-configurable, default to "redhat-ods-applications"
	// +kubebuilder:default=redhat-ods-applications
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ApplicationsNamespace is immutable"
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	ApplicationsNamespace string `json:"applicationsNamespace,omitempty"`
	// Enable monitoring on specified namespace
	// +optional
	Monitoring serviceApi.DSCIMonitoring `json:"monitoring,omitempty"`
	// When set to `Managed`, adds odh-trusted-ca-bundle Configmap to all namespaces that includes
	// cluster-wide Trusted CA Bundle in .data["ca-bundle.crt"].
	// Additionally, this fields allows admins to add custom CA bundles to the configmap using the .CustomCABundle field.
	// +optional
	TrustedCABundle *TrustedCABundleSpec `json:"trustedCABundle,omitempty"`
	// Internal development useful field to test customizations.
	// This is not recommended to be used in production environment.
	// +optional
	DevFlags *DevFlags `json:"devFlags,omitempty"`
}
