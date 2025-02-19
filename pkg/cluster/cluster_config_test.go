package cluster_test

import (
	"context"
	"reflect"
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
	ctx := context.Background()

	nodeTypeMeta := metav1.TypeMeta{
		APIVersion: gvk.Node.GroupVersion().String(),
		Kind:       gvk.Node.GroupVersion().String(),
	}
	nodeStatusReady := corev1.NodeStatus{
		Conditions: []corev1.NodeCondition{
			{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			},
		},
	}

	amdNode := &corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "amd-node",
			Labels: map[string]string{
				labels.NodeArch: "amd64",
			},
		},
		Status: nodeStatusReady,
	}
	powerNode := &corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "power-node",
			Labels: map[string]string{
				labels.NodeArch: "ppc64le",
			},
		},
		Status: nodeStatusReady,
	}
	notReadyNode := &corev1.Node{
		TypeMeta: nodeTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: "not-ready-node",
			Labels: map[string]string{
				labels.NodeArch: "amd64",
			},
		},
	}

	type args struct {
		client client.Client
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]struct{}
		wantErr bool
	}{
		{
			name: "Single-node cluster",
			args: args{
				client: fake.NewFakeClient(amdNode),
			},
			want: map[string]struct{}{
				"amd64": {},
			},
			wantErr: false,
		},
		{
			name: "Multi-node cluster",
			args: args{
				client: fake.NewFakeClient(amdNode, powerNode),
			},
			want: map[string]struct{}{
				"amd64":   {},
				"ppc64le": {},
			},
			wantErr: false,
		},
		{
			name: "No ready nodes cluster",
			args: args{
				client: fake.NewFakeClient(notReadyNode),
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cluster.GetNodeArchitectures(ctx, tt.args.client)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNodeArchitectures() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetNodeArchitectures() got = %v, want %v", got, tt.want)
			}
		})
	}
}
