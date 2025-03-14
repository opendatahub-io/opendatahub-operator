package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
)

func deletionTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("Deletion DSC instance", func(t *testing.T) {
			err = testCtx.testDeletionExistDSC()
			require.NoError(t, err, "Error to delete DSC instance")
		})

		t.Run("Deletion DSCI instance", func(t *testing.T) {
			err = testCtx.testDeletionExistDSCI()
			require.NoError(t, err, "Error to delete DSCI instance")
		})
	})
}

func (tc *testContext) testDeletionExistDSC() error {
	// Delete test DataScienceCluster resource if found

	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	expectedDSC := &dscv1.DataScienceCluster{}

	err := tc.customClient.Get(tc.ctx, dscLookupKey, expectedDSC)
	if err == nil {
		dscerr := tc.customClient.Delete(tc.ctx, expectedDSC, &client.DeleteOptions{})
		if dscerr != nil {
			return fmt.Errorf("error deleting DSC instance %s: %w", expectedDSC.Name, dscerr)
		}
	} else if !k8serr.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("could not find DSC instance to delete: %w", err)
		}
	}

	return nil
}

// To test if  DSCI CR is in the cluster and no problem to delete it
// if fail on any of these two conditios, fail test.
func (tc *testContext) testDeletionExistDSCI() error {
	// Delete test DSCI resource if found

	dsciLookupKey := types.NamespacedName{Name: tc.testDSCI.Name}
	expectedDSCI := &dsciv1.DSCInitialization{}

	err := tc.customClient.Get(tc.ctx, dsciLookupKey, expectedDSCI)
	if err == nil {
		dscierr := tc.customClient.Delete(tc.ctx, expectedDSCI, &client.DeleteOptions{})
		if dscierr != nil {
			return fmt.Errorf("error deleting DSCI instance %s: %w", expectedDSCI.Name, dscierr)
		}
	} else if !k8serr.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("could not find DSCI instance to delete :%w", err)
		}
	}

	return nil
}
