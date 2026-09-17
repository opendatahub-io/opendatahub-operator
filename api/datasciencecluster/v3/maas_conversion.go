package v3

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
)

// MaaSV2StateAnnotation records legacy-only v2 MaaS enablement.
// It is conversion-owned metadata, not user configuration.
const MaaSV2StateAnnotation = "conversion.opendatahub.io/maas-v2-state"

const legacyMaaSManaged = "legacy-managed"

// PreserveMaaSV2State derives legacy Managed provenance from visible v2 state,
// never from incoming annotations. The marker records the legacy source even
// when the KServe parent prevents that legacy state from becoming effective.
func (dsc *DataScienceCluster) PreserveMaaSV2State(canonical, legacy operatorv1.ManagementState) {
	delete(dsc.Annotations, MaaSV2StateAnnotation)

	if canonical == operatorv1.Managed || legacy != operatorv1.Managed {
		return
	}

	if dsc.Annotations == nil {
		dsc.Annotations = make(map[string]string)
	}

	dsc.Annotations[MaaSV2StateAnnotation] = legacyMaaSManaged
}

// ReadMaaSV2State reports legacy Managed provenance and rejects unknown markers.
func (dsc *DataScienceCluster) ReadMaaSV2State() (bool, error) {
	value, found := dsc.Annotations[MaaSV2StateAnnotation]
	if !found {
		return false, nil
	}

	if value != legacyMaaSManaged {
		return false, fmt.Errorf("invalid MaaS v2 provenance marker %q", value)
	}

	return true, nil
}

// AdmitMaaSV2State preserves authoritative old provenance across unrelated
// updates. Native creates and canonical MaaS edits retire legacy configuration.
func (dsc *DataScienceCluster) AdmitMaaSV2State(old *DataScienceCluster) error {
	if old == nil {
		dsc.PreserveMaaSV2State(dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState, operatorv1.Removed)

		return nil
	}

	managed, err := old.ReadMaaSV2State()
	if err != nil {
		return err
	}

	if managed && old.Spec.Components.AIGateway.ModelsAsAService.ManagementState ==
		dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState {
		dsc.PreserveMaaSV2State(operatorv1.Removed, operatorv1.Managed)

		return nil
	}

	dsc.PreserveMaaSV2State(dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState, operatorv1.Removed)

	return nil
}

// AdmitMaaSV2StateFromV2 applies the provenance produced while converting a
// v2 request. A marker produced by that conversion is authoritative; when no
// marker is present, the v2 request explicitly supplied canonical state and
// must retire any stored legacy provenance.
func (dsc *DataScienceCluster) AdmitMaaSV2StateFromV2() error {
	managed, err := dsc.ReadMaaSV2State()
	if err != nil {
		return err
	}

	if managed {
		return nil
	}

	dsc.PreserveMaaSV2State(dsc.Spec.Components.AIGateway.ModelsAsAService.ManagementState, operatorv1.Removed)

	return nil
}
