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

package v2

import (
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this DSCInitialization (v2) to the Hub version (v1).
func (c *DSCInitialization) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dsciv1.DSCInitialization)

	dst.TypeMeta = c.TypeMeta
	dst.ObjectMeta = c.ObjectMeta

	dst.Spec = dsciv1.DSCInitializationSpec{
		ApplicationsNamespace: c.Spec.ApplicationsNamespace,
		Monitoring:            c.Spec.Monitoring,
		ServiceMesh:           c.Spec.ServiceMesh,
		TrustedCABundle: &dsciv1.TrustedCABundleSpec{
			ManagementState: c.Spec.TrustedCABundle.ManagementState,
			CustomCABundle:  c.Spec.TrustedCABundle.CustomCABundle,
		},
		DevFlags: &dsciv1.DevFlags{
			ManifestsUri: c.Spec.DevFlags.ManifestsUri,
			LogMode:      c.Spec.DevFlags.LogMode,
			LogLevel:     c.Spec.DevFlags.LogLevel,
		},
	}

	dst.Status = dsciv1.DSCInitializationStatus{
		Phase:          c.Status.Phase,
		Conditions:     c.Status.Conditions,
		RelatedObjects: c.Status.RelatedObjects,
		ErrorMessage:   c.Status.ErrorMessage,
		Release:        c.Status.Release,
	}

	return nil
}

// ConvertFrom converts the Hub version (v1) to this DSCInitialization (v2).
func (c *DSCInitialization) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*dsciv1.DSCInitialization)

	c.TypeMeta = src.TypeMeta
	c.ObjectMeta = src.ObjectMeta

	c.Spec = DSCInitializationSpec{
		ApplicationsNamespace: src.Spec.ApplicationsNamespace,
		Monitoring:            src.Spec.Monitoring,
		ServiceMesh:           src.Spec.ServiceMesh,
		TrustedCABundle:       (*TrustedCABundleSpec)(src.Spec.TrustedCABundle),
		DevFlags:              (*DevFlags)(src.Spec.DevFlags),
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
