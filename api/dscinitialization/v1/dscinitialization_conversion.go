/*
Copyright 2023.

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

package v1

import (
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this DSCInitialization (v1) to the Hub version (v2).
func (c *DSCInitialization) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dsciv2.DSCInitialization)

	dst.ObjectMeta = c.ObjectMeta

	dst.Spec = dsciv2.DSCInitializationSpec{
		ApplicationsNamespace: c.Spec.ApplicationsNamespace,
		Monitoring:            c.Spec.Monitoring,
		ServiceMesh:           c.Spec.ServiceMesh,
	}
	if c.Spec.TrustedCABundle != nil {
		dst.Spec.TrustedCABundle = &dsciv2.TrustedCABundleSpec{
			ManagementState: c.Spec.TrustedCABundle.ManagementState,
			CustomCABundle:  c.Spec.TrustedCABundle.CustomCABundle,
		}
	}
	if c.Spec.DevFlags != nil {
		dst.Spec.DevFlags = &dsciv2.DevFlags{
			ManifestsUri: c.Spec.DevFlags.ManifestsUri,
			LogMode:      c.Spec.DevFlags.LogMode,
			LogLevel:     c.Spec.DevFlags.LogLevel,
		}
	}

	dst.Status = dsciv2.DSCInitializationStatus{
		Phase:          c.Status.Phase,
		Conditions:     c.Status.Conditions,
		RelatedObjects: c.Status.RelatedObjects,
		ErrorMessage:   c.Status.ErrorMessage,
		Release:        c.Status.Release,
	}

	return nil
}

// ConvertFrom converts the Hub version (v2) to this DSCInitialization (v1).
func (c *DSCInitialization) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*dsciv2.DSCInitialization)

	c.ObjectMeta = src.ObjectMeta

	c.Spec = DSCInitializationSpec{
		ApplicationsNamespace: src.Spec.ApplicationsNamespace,
		Monitoring:            src.Spec.Monitoring,
		ServiceMesh:           src.Spec.ServiceMesh,
	}
	if src.Spec.TrustedCABundle != nil {
		c.Spec.TrustedCABundle = &TrustedCABundleSpec{
			ManagementState: src.Spec.TrustedCABundle.ManagementState,
			CustomCABundle:  src.Spec.TrustedCABundle.CustomCABundle,
		}
	}
	if src.Spec.DevFlags != nil {
		c.Spec.DevFlags = &DevFlags{
			ManifestsUri: src.Spec.DevFlags.ManifestsUri,
			LogMode:      src.Spec.DevFlags.LogMode,
			LogLevel:     src.Spec.DevFlags.LogLevel,
		}
	}

	c.Status = DSCInitializationStatus{
		Phase:          src.Status.Phase,
		Conditions:     src.Status.Conditions,
		RelatedObjects: src.Status.RelatedObjects,
		ErrorMessage:   src.Status.ErrorMessage,
		Release:        src.Status.Release,
	}

	return nil
}
