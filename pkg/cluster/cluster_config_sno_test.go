package cluster_test

import (
	"context"
	"errors"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// snoTestClient wraps a fake client to inject specific errors for Infrastructure Get and Node List.
type snoTestClient struct {
	client.Client

	infraErr error
	listErr  error
}

func (c *snoTestClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*configv1.Infrastructure); ok {
		if c.infraErr != nil {
			return c.infraErr
		}
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *snoTestClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.listErr != nil {
		if _, ok := list.(*corev1.NodeList); ok {
			return c.listErr
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func newNode(name string, unschedulable bool) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.NodeSpec{Unschedulable: unschedulable},
	}
}

func newInfrastructure(topology configv1.TopologyMode) *configv1.Infrastructure {
	return &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Status: configv1.InfrastructureStatus{
			ControlPlaneTopology: topology,
		},
	}
}

func TestIsSingleNodeCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := configv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add configv1 scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}

	noMatchErr := &meta.NoKindMatchError{
		GroupKind: schema.GroupKind{
			Group: "config.openshift.io",
			Kind:  "Infrastructure",
		},
		SearchedVersions: []string{"v1"},
	}

	testCases := []struct {
		name     string
		infra    *configv1.Infrastructure
		infraErr error
		nodes    []*corev1.Node
		listErr  error
		expected bool
	}{
		{
			name:     "OpenShift SNO - SingleReplica topology",
			infra:    newInfrastructure(configv1.SingleReplicaTopologyMode),
			expected: true,
		},
		{
			name:     "OpenShift multi-node - HighlyAvailable topology",
			infra:    newInfrastructure(configv1.HighlyAvailableTopologyMode),
			expected: false,
		},
		{
			name:     "OpenShift - empty topology (defaults to multi-node)",
			infra:    newInfrastructure(""),
			expected: false,
		},
		{
			name:     "Infrastructure not found - fallback single schedulable node",
			infraErr: k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster"),
			nodes:    []*corev1.Node{newNode("node-1", false)},
			expected: true,
		},
		{
			name:     "Infrastructure not found - fallback multiple schedulable nodes",
			infraErr: k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster"),
			nodes: []*corev1.Node{
				newNode("node-1", false),
				newNode("node-2", false),
				newNode("node-3", false),
			},
			expected: false,
		},
		{
			name:     "Infrastructure CRD absent (NoMatch) - fallback single node",
			infraErr: noMatchErr,
			nodes:    []*corev1.Node{newNode("node-1", false)},
			expected: true,
		},
		{
			name:     "Infrastructure CRD absent (NoMatch) - fallback multiple nodes",
			infraErr: noMatchErr,
			nodes: []*corev1.Node{
				newNode("node-1", false),
				newNode("node-2", false),
			},
			expected: false,
		},
		{
			name:     "Fallback - one schedulable and one unschedulable node",
			infraErr: k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster"),
			nodes: []*corev1.Node{
				newNode("node-1", false),
				newNode("node-2", true),
			},
			expected: true,
		},
		{
			name:     "Fallback - all nodes unschedulable",
			infraErr: k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster"),
			nodes: []*corev1.Node{
				newNode("node-1", true),
				newNode("node-2", true),
			},
			expected: false,
		},
		{
			name:     "Fallback - no nodes at all",
			infraErr: k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster"),
			nodes:    []*corev1.Node{},
			expected: false,
		},
		{
			name:     "Other Infrastructure error - defaults to multi-node (no fallback)",
			infraErr: errors.New("connection refused"),
			nodes:    []*corev1.Node{newNode("node-1", false)},
			expected: false,
		},
		{
			name:     "Infrastructure not found - node list error defaults to multi-node",
			infraErr: k8serr.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster"),
			listErr:  errors.New("forbidden"),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			objs := []runtime.Object{}
			if tc.infra != nil {
				objs = append(objs, tc.infra)
			}
			for _, node := range tc.nodes {
				objs = append(objs, node)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			var cli client.Client = fakeClient
			if tc.infraErr != nil || tc.listErr != nil {
				cli = &snoTestClient{
					Client:   fakeClient,
					infraErr: tc.infraErr,
					listErr:  tc.listErr,
				}
			}

			result := cluster.IsSingleNodeCluster(ctx, cli)
			if result != tc.expected {
				t.Errorf("IsSingleNodeCluster() = %v, want %v", result, tc.expected)
			}
		})
	}
}
