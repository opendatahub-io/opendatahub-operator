package v2

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
)

// DataConnectHubV3StateAnnotation preserves v3-only DCH settings through the v2
// view, which has no DCH stanza. It is conversion-owned metadata.
const DataConnectHubV3StateAnnotation = "conversion.opendatahub.io/dataconnecthub-v3-state"

func emptyDataConnectHubState(config componentApi.DSCDataConnectHub) bool {
	return config.ManagementState == "" && config.Namespace == "" && config.Gateway == nil && len(config.TokenReviewAudiences) == 0
}

// validateDataConnectHubState mirrors the v3 field schema because annotation
// values written through v2 are not validated by the v3 OpenAPI schema.
func validateDataConnectHubState(config componentApi.DSCDataConnectHub) error {
	if config.Namespace != "" && len(validation.IsDNS1123Label(config.Namespace)) > 0 {
		return fmt.Errorf("invalid Data Connect Hub namespace %q", config.Namespace)
	}
	if config.ManagementState != "" && config.ManagementState != operatorv1.Managed && config.ManagementState != operatorv1.Removed {
		return fmt.Errorf("invalid Data Connect Hub management state %q", config.ManagementState)
	}
	if config.Gateway != nil {
		if strings.TrimSpace(config.Gateway.Name) == "" || strings.TrimSpace(config.Gateway.Namespace) == "" {
			return fmt.Errorf("Data Connect Hub gateway name and namespace must not be empty")
		}
		if utf8.RuneCountInString(config.Gateway.Name) > 253 || utf8.RuneCountInString(config.Gateway.Namespace) > 63 {
			return fmt.Errorf("Data Connect Hub gateway exceeds its maximum length")
		}
	}
	if len(config.TokenReviewAudiences) > 32 {
		return fmt.Errorf("Data Connect Hub supports at most 32 TokenReview audiences")
	}
	seen := make(map[string]bool, len(config.TokenReviewAudiences))
	for _, audience := range config.TokenReviewAudiences {
		if strings.TrimSpace(audience) == "" || utf8.RuneCountInString(audience) > 256 || seen[audience] {
			return fmt.Errorf("Data Connect Hub TokenReview audiences must be non-empty, unique, and at most 256 characters")
		}
		seen[audience] = true
	}
	return nil
}

func restoreDataConnectHubState(dst *dscApi.DataScienceCluster) error {
	value, found := dst.Annotations[DataConnectHubV3StateAnnotation]
	if !found {
		return nil
	}
	var config componentApi.DSCDataConnectHub
	if err := json.Unmarshal([]byte(value), &config); err != nil {
		// Accept the original development marker containing only a scalar state.
		config.ManagementState = operatorv1.ManagementState(value)
	}
	if emptyDataConnectHubState(config) {
		return fmt.Errorf("Data Connect Hub v3 state marker contains no settings")
	}
	if err := validateDataConnectHubState(config); err != nil {
		return err
	}
	dst.Spec.Components.Data.DataConnectHub = config
	delete(dst.Annotations, DataConnectHubV3StateAnnotation)
	if len(dst.Annotations) == 0 {
		dst.Annotations = nil
	}
	return nil
}

func preserveDataConnectHubState(dst *DataScienceCluster, src *dscApi.DataScienceCluster) error {
	delete(dst.Annotations, DataConnectHubV3StateAnnotation)
	config := src.Spec.Components.Data.DataConnectHub
	if err := validateDataConnectHubState(config); err != nil {
		return err
	}
	if !emptyDataConnectHubState(config) {
		encoded, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("encoding Data Connect Hub v3 state: %w", err)
		}
		if dst.Annotations == nil {
			dst.Annotations = make(map[string]string)
		}
		dst.Annotations[DataConnectHubV3StateAnnotation] = string(encoded)
	}
	return nil
}
