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
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this DataScienceCluster (v1) to the Hub version (v2).
func (c *DataScienceCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*dscv2.DataScienceCluster)

	dst.ObjectMeta = c.ObjectMeta

	dst.Spec = dscv2.DataScienceClusterSpec{
		Components: dscv2.Components(c.Spec.Components),
	}

	dst.Status = dscv2.DataScienceClusterStatus{
		Status:              c.Status.Status,
		RelatedObjects:      c.Status.RelatedObjects,
		ErrorMessage:        c.Status.ErrorMessage,
		InstalledComponents: c.Status.InstalledComponents,
		Components:          dscv2.ComponentsStatus(c.Status.Components),
		Release:             c.Status.Release,
	}

	return nil
}

// ConvertFrom converts the Hub version (v2) to this DataScienceCluster (v1).
func (c *DataScienceCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*dscv2.DataScienceCluster)

	c.ObjectMeta = src.ObjectMeta

	c.Spec = DataScienceClusterSpec{
		Components: Components(src.Spec.Components),
	}

	c.Status = DataScienceClusterStatus{
		Status:              src.Status.Status,
		RelatedObjects:      src.Status.RelatedObjects,
		ErrorMessage:        src.Status.ErrorMessage,
		InstalledComponents: src.Status.InstalledComponents,
		Components:          ComponentsStatus(src.Status.Components),
		Release:             src.Status.Release,
	}

	return nil
}
