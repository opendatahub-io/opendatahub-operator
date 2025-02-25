package architecture

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	DefaultArchitecture = "amd64"
)

// VerifySupportedArchitectures determines whether a component can be enabled based on the architecture of each node.
//
// This is accomplished by doing the following:
// 1. Fetching the architecture(s) that the component supports.
// 2. Fetching the architecture(s) that the nodes are running on.
// 3. Verifying that there is at least one node that is running an architecture supported by the component.
func VerifySupportedArchitectures(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	obj, ok := rr.Instance.(common.WithReleases)
	if !ok {
		return fmt.Errorf("instance %v is not a WithReleases", rr.Instance)
	}

	// Fetch the architecture(s) that the component supports
	var supportedArchitectures []string
	componentReleases := obj.GetReleaseStatus()
	if componentReleases == nil || len(*componentReleases) < 1 {
		return fmt.Errorf("instance %v has no releases", rr.Instance)
	}
	for _, release := range *componentReleases {
		supportedArchitectures = append(supportedArchitectures, release.SupportedArchitectures...)
	}

	// TODO: Refactor after all components explicitly list supportedArchitectures in their component_metadata.yaml file
	// If supportedArchitectures is empty, assume the component only works on amd64
	if len(supportedArchitectures) == 0 {
		supportedArchitectures = append(supportedArchitectures, DefaultArchitecture)
	}

	// Fetch the ready worker nodes
	readyWorkerNodes, err := cluster.GetReadyWorkerNodes(ctx, rr.Client)
	if err != nil {
		return err
	}

	// Fetch the architecture(s) that the nodes are running on
	nodeArchitectures, err := cluster.GetNodeArchitectures(readyWorkerNodes)
	if err != nil {
		return err
	}

	componentSchedulable := HasCompatibleArchitecture(supportedArchitectures, nodeArchitectures)
	if !componentSchedulable {
		s := rr.Instance.GetStatus()
		s.Phase = "NotReady"

		conditions.SetStatusCondition(rr.Instance, common.Condition{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             status.UnsupportedArchitectureReason,
			Message:            status.UnsupportedArchitectureMessage,
			ObservedGeneration: s.ObservedGeneration,
		})

		return odherrors.NewStopError(status.UnsupportedArchitectureMessage)
	}

	return nil
}

// HasCompatibleArchitecture Returns true if there's at least one architecture that's in both supportedArches and nodeArches.
// Otherwise, it returns false.
func HasCompatibleArchitecture(supportedArches []string, nodeArches []string) bool {
	for _, nodeArch := range nodeArches {
		for _, supportedArch := range supportedArches {
			if nodeArch == supportedArch {
				return true
			}
		}
	}

	return false
}
