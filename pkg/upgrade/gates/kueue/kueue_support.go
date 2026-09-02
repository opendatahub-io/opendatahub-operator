package kueue

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

func ManagementState(ctx context.Context, reader client.Reader) (string, error) {
	dsc, err := cluster.GetDSC(ctx, reader)
	switch {
	case k8serr.IsNotFound(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("getting DataScienceCluster for Kueue managementState: %w", err)
	}

	state := dsc.Spec.Components.Kueue.ManagementState
	if state == "" {
		state = operatorv1.Removed
	}

	return string(state), nil
}
