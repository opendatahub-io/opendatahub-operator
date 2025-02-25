package cluster_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func TestGetNodeArchitectures(t *testing.T) {
	nodeTypeMeta := metav1.TypeMeta{
		APIVersion: gvk.Node.GroupVersion().String(),
		Kind:       gvk.Node.GroupKind().String(),
	}

	amdNode := corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "amd-node",
			Labels: map[string]string{
				labels.NodeArch: "amd64",
			},
		},
	}
	powerNode := corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "power-node",
			Labels: map[string]string{
				labels.NodeArch: "ppc64le",
			},
		},
	}
	unlabeledNode := corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "power-node",
		},
	}

	type args struct {
		nodes []corev1.Node
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "Single-arch nodes",
			args: args{
				nodes: []corev1.Node{amdNode},
			},
			want: []string{
				"amd64",
			},
			wantErr: false,
		},
		{
			name: "Multi-arch nodes",
			args: args{
				nodes: []corev1.Node{amdNode, powerNode},
			},
			want: []string{
				"amd64",
				"ppc64le",
			},
			wantErr: false,
		},
		{
			name: "Unlabeled nodes",
			args: args{
				nodes: []corev1.Node{unlabeledNode},
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cluster.GetNodeArchitectures(tt.args.nodes)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNodeArchitectures() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Sort both got and tt.want slices to ignore order differences
			if !tt.wantErr {
				sort.Strings(got)
				sort.Strings(tt.want)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetNodeArchitectures() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetReadyWorkerNodes(t *testing.T) {
	ctx := context.Background()

	nodeTypeMeta := metav1.TypeMeta{
		APIVersion: gvk.Node.GroupVersion().String(),
		Kind:       gvk.Node.GroupKind().String(),
	}

	readyWorkerNode := corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "ready-worker-node",
			Labels: map[string]string{
				labels.WorkerNode: "",
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	notReadyWorkerNode := corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "not-ready-worker-node",
			Labels: map[string]string{
				labels.WorkerNode: "",
			},
		},
	}
	masterNode := corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "master-node",
			Labels: map[string]string{
				"node-role.kubernetes.io/master": "",
			},
		},
	}

	type args struct {
		k8sclient client.Client
	}
	tests := []struct {
		name    string
		args    args
		want    []corev1.Node
		wantErr bool
	}{
		{
			name: "Ready worker nodes",
			args: args{
				k8sclient: fake.NewFakeClient(&readyWorkerNode, &masterNode, &notReadyWorkerNode),
			},
			want:    []corev1.Node{readyWorkerNode},
			wantErr: false,
		},
		{
			name: "No worker nodes",
			args: args{
				k8sclient: fake.NewFakeClient(&masterNode),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "No ready worker nodes",
			args: args{
				k8sclient: fake.NewFakeClient(&notReadyWorkerNode),
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cluster.GetReadyWorkerNodes(ctx, tt.args.k8sclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReadyWorkerNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetReadyWorkerNodes() got = %v, want %v", got, tt.want)
			}
		})
	}
}
