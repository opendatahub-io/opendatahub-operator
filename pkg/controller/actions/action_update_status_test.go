package actions_test

import (
	"context"
	"testing"

	"github.com/onsi/gomega/gstruct"
	"github.com/rs/xid"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	. "github.com/onsi/gomega"
)

func TestUpdateStatusAction(t *testing.T) {
	_ = NewWithT(t)

	testCases := []struct {
		name                 string
		deployments          []appsv1.Deployment
		expectedStatus       metav1.ConditionStatus
		expectedReason       string
		additionalAssertions func(*testing.T, *types.ReconciliationRequest)
	}{
		{
			name: "Not Ready - One deployment not ready",
			deployments: []appsv1.Deployment{
				createDeployment("my-deployment", 1, 0),
				createDeployment("my-deployment-2", 1, 1),
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: actions.DeploymentsNotReadyReason,
		},
		{
			name: "Ready - All deployments ready",
			deployments: []appsv1.Deployment{
				createDeployment("my-deployment", 1, 1),
				createDeployment("my-deployment-2", 2, 2),
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: actions.ReadyReason,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			ns := xid.New().String()

			cli := NewFakeClient(createObjectList(tc.deployments)...)

			action := actions.NewUpdateStatusAction(
				ctx,
				actions.WithUpdateStatusLabel(labels.K8SCommon.PartOf, "foo"))

			rr := types.ReconciliationRequest{
				Client:   cli,
				Instance: &componentsv1.Dashboard{},
				DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
				DSC:      &dscv1.DataScienceCluster{},
				Platform: cluster.OpenDataHub,
			}

			err := action.Execute(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			g.Expect(rr.Instance).Should(
				WithTransform(
					ExtractStatusCondition(status.ConditionTypeReady),
					gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"Status": Equal(tc.expectedStatus),
						"Reason": Equal(tc.expectedReason),
					}),
				),
			)

			if tc.additionalAssertions != nil {
				tc.additionalAssertions(t, &rr)
			}
		})
	}
}

// Helper functions

func createDeployment(name string, replicas, readyReplicas int32) appsv1.Deployment {
	return appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.Deployment.GroupVersion().String(),
			Kind:       gvk.Deployment.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				labels.K8SCommon.PartOf: "foo",
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      replicas,
			ReadyReplicas: readyReplicas,
		},
	}
}

func createObjectList(deployments []appsv1.Deployment) []client.Object {
	objects := make([]client.Object, len(deployments))
	for i := range deployments {
		objects[i] = &deployments[i]
	}
	return objects
}
