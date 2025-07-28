package kueue_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/rs/xid"
	k8serr "k8s.io/apimachinery/pkg/api/errors"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

var (
	kueueQueueNameLabelKey     = cluster.KueueQueueNameLabel
	localQueueName             = "default"
	kueueManagedLabelKey       = cluster.KueueManagedLabelKey
	kueueLegacyManagedLabelKey = cluster.KueueLegacyManagedLabelKey
	missingLabelError          = `Kueue label validation failed: missing required label "` + kueueQueueNameLabelKey + `"`
)

func TestKueueWebhook_Integration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		kueueState        operatorv1.ManagementState
		nsLabels          map[string]string
		workloadLabels    map[string]string
		expectAllowed     bool
		expectDeniedError string
	}{
		{
			name:           "Kueue disabled in DSC - should allow",
			kueueState:     operatorv1.Removed,
			nsLabels:       map[string]string{kueueManagedLabelKey: "true"},
			workloadLabels: map[string]string{},
			expectAllowed:  true,
		},
		{
			name:              "Kueue enabled, ns enabled, missing workload label - should deny",
			kueueState:        operatorv1.Managed,
			nsLabels:          map[string]string{kueueManagedLabelKey: "true"},
			workloadLabels:    map[string]string{},
			expectAllowed:     false,
			expectDeniedError: missingLabelError,
		},
		{
			name:           "Kueue enabled, ns enabled, valid workload label - should allow",
			kueueState:     operatorv1.Managed,
			nsLabels:       map[string]string{kueueManagedLabelKey: "true"},
			workloadLabels: map[string]string{kueueQueueNameLabelKey: localQueueName},
			expectAllowed:  true,
		},
		{
			name:           "Kueue enabled, ns not labeled - should allow",
			kueueState:     operatorv1.Managed,
			nsLabels:       nil,
			workloadLabels: map[string]string{},
			expectAllowed:  true,
		},
		{
			name:           "Kueue enabled, ns enabled with legacy label, valid workload label - should allow",
			kueueState:     operatorv1.Managed,
			nsLabels:       map[string]string{kueueLegacyManagedLabelKey: "true"},
			workloadLabels: map[string]string{kueueQueueNameLabelKey: localQueueName},
			expectAllowed:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{
					envtestutil.RegisterHardwareProfileAndKueueWebhooks,
					dscwebhook.RegisterWebhooks,
				},
				20*time.Second,
				envtestutil.WithNotebook(),
			)

			t.Cleanup(teardown)
			k8sClient := env.Client()

			ns := xid.New().String()

			// Create DSC with the appropriate Kueue state
			dsc := envtestutil.NewDSC("default", "")
			g.Expect(k8sClient.Create(ctx, dsc)).To(Succeed())

			// Update status separately (required for envtest)
			dsc.Status.Components.Kueue = componentApi.DSCKueueStatus{
				KueueManagementSpec: componentApi.KueueManagementSpec{
					ManagementState: tc.kueueState,
				},
			}
			g.Expect(k8sClient.Status().Update(ctx, dsc)).To(Succeed())

			g.Expect(k8sClient.Create(ctx, envtestutil.NewNamespace(ns, tc.nsLabels))).To(Succeed())

			workload := envtestutil.NewNotebook("test-notebook", ns, envtestutil.WithLabels(tc.workloadLabels))
			err := k8sClient.Create(ctx, workload)

			if tc.expectAllowed {
				g.Expect(err).To(Succeed(), fmt.Sprintf("Expected creation to be allowed but got: %v", err))
			} else {
				g.Expect(err).To(HaveOccurred(), "Expected creation to be denied but it was allowed.")
				statusErr := &k8serr.StatusError{}
				ok := errors.As(err, &statusErr)
				g.Expect(ok).To(BeTrue(), "Expected error to be of type StatusError")
				g.Expect(statusErr.Status().Code).To(Equal(int32(http.StatusForbidden)))
				g.Expect(statusErr.Status().Message).To(ContainSubstring(tc.expectDeniedError))
			}
		})
	}
}
