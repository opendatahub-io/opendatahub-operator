//nolint:testpackage
package kueue

import (
	"context"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestConfigureClusterQueueViewerRoleAction_RoleNotFound(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kueue{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = configureClusterQueueViewerRoleAction(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
}

func TestConfigureClusterQueueViewerRoleAction(t *testing.T) {
	roleWithTrueLabel := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: map[string]string{KueueBatchUserLabel: "true"},
		},
	}
	roleWithFalseLabel := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: map[string]string{KueueBatchUserLabel: "false"},
		},
	}
	roleWithMissingLabel := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: map[string]string{},
		},
	}
	roleWithNilLabels := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ClusterQueueViewerRoleName,
			Labels: nil,
		},
	}
	var tests = []struct {
		name        string
		clusterRole *rbacv1.ClusterRole
	}{
		{"labelIsTrue", roleWithTrueLabel},
		{"labelIsFalse", roleWithFalseLabel},
		{"labelIsMissing", roleWithMissingLabel},
		{"labelsNil", roleWithNilLabels},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			g := NewWithT(t)

			cli, err := fakeclient.New(fakeclient.WithObjects(test.clusterRole))
			g.Expect(err).ShouldNot(HaveOccurred())

			ks := componentApi.Kueue{}

			rr := types.ReconciliationRequest{
				Client:     cli,
				Instance:   &ks,
				Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
			}

			err = configureClusterQueueViewerRoleAction(ctx, &rr)
			g.Expect(err).ShouldNot(HaveOccurred())
			err = cli.Get(ctx, client.ObjectKeyFromObject(test.clusterRole), test.clusterRole)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(test.clusterRole.Labels[KueueBatchUserLabel]).Should(Equal("true"))
		})
	}
}
