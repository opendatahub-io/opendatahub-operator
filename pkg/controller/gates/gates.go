package gates

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed resources
var gateResourcesFS embed.FS

const (
	UpgradeGateLabel    = "platform.opendatahub.io/upgrade-gate"
	AcksConfigMap       = "odh-upgrade-acks"
	ManagedByAnnotation = "platform.opendatahub.io/managed-by"
)

// GateChecker evaluates admin acknowledgment gates as an upgrade
// precondition. Gate descriptions are written to the odh-upgrade-acks
// ConfigMap; admin acknowledges by setting a gate's value to "true".
type GateChecker struct {
	client    client.Client
	namespace string
}

// NewGateChecker creates a new GateChecker for the given namespace.
func NewGateChecker(cli client.Client, namespace string) *GateChecker {
	return &GateChecker{client: cli, namespace: namespace}
}

// UnackedGate represents an upgrade gate that has not been acknowledged.
type UnackedGate struct {
	Key     string
	Message string
}

// EnsureGates writes gate descriptions into the odh-upgrade-acks
// ConfigMap for all entries matching the version prefix
// "ack-<version>-". Entries already set to "true" (admin-acknowledged)
// are never overwritten. Returns the list of unacknowledged gates.
//
// This single method replaces the former AggregateGates + CheckGates
// two-ConfigMap flow. The acks ConfigMap now serves double duty: its
// values are either a descriptive message (unacked) or "true" (acked).
func (gc *GateChecker) EnsureGates(ctx context.Context, gateEntries map[string]string, version string) ([]UnackedGate, error) {
	if version == "" {
		return nil, errors.New("version must not be empty")
	}

	versionPrefix := "ack-" + version + "-"

	filtered := make(map[string]string)
	for k, v := range gateEntries {
		if strings.HasPrefix(k, versionPrefix) {
			filtered[k] = v
		}
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	cm := &corev1.ConfigMap{}
	err := gc.client.Get(ctx, client.ObjectKey{
		Name:      AcksConfigMap,
		Namespace: gc.namespace,
	}, cm)

	if k8serr.IsNotFound(err) {
		// No OwnerReference: the acks ConfigMap is a cluster singleton
		// whose lifecycle is independent of any CR. Admin acknowledgments
		// must survive CR deletion and re-creation.
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      AcksConfigMap,
				Namespace: gc.namespace,
				Annotations: map[string]string{
					ManagedByAnnotation: "opendatahub-operator",
				},
			},
			Data: make(map[string]string, len(filtered)),
		}
		maps.Copy(cm.Data, filtered)
		if createErr := gc.client.Create(ctx, cm); createErr != nil {
			if !k8serr.IsAlreadyExists(createErr) {
				return nil, fmt.Errorf("failed to create %s ConfigMap: %w", AcksConfigMap, createErr)
			}
			if err := gc.client.Get(ctx, client.ObjectKey{Name: AcksConfigMap, Namespace: gc.namespace}, cm); err != nil {
				return nil, fmt.Errorf("failed to get %s ConfigMap after race: %w", AcksConfigMap, err)
			}
		} else {
			return gc.collectUnacked(cm, filtered), nil
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get %s ConfigMap: %w", AcksConfigMap, err)
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string, len(filtered))
	}

	dirty := false
	for k, v := range filtered {
		if cm.Data[k] == "true" {
			continue
		}
		if cm.Data[k] != v {
			cm.Data[k] = v
			dirty = true
		}
	}

	if dirty {
		if err := gc.client.Update(ctx, cm); err != nil {
			return nil, fmt.Errorf("failed to update %s ConfigMap: %w", AcksConfigMap, err)
		}
	}

	return gc.collectUnacked(cm, filtered), nil
}

func (gc *GateChecker) collectUnacked(cm *corev1.ConfigMap, filtered map[string]string) []UnackedGate {
	var unacked []UnackedGate
	for key, message := range filtered {
		if cm.Data[key] != "true" {
			unacked = append(unacked, UnackedGate{Key: key, Message: message})
		}
	}

	sort.Slice(unacked, func(i, j int) bool {
		return unacked[i].Key < unacked[j].Key
	})

	return unacked
}

// IsGateConfigMap returns true if the given ConfigMap has the upgrade gate
// label, indicating it should be extracted during chart rendering.
func IsGateConfigMap(cm *corev1.ConfigMap) bool {
	if cm == nil || cm.Labels == nil {
		return false
	}
	return cm.Labels[UpgradeGateLabel] == "true"
}

// LoadInTreeGates reads embedded YAML gate definitions from the
// resources/ directory and returns all gate entries whose key starts
// with the version prefix "ack-<version>-". This provides gate
// definitions for components that have not yet migrated to modules.
//
// Temporary: remove when all components are modules.
func LoadInTreeGates(version string) (map[string]string, error) {
	entries, err := gateResourcesFS.ReadDir("resources")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded gate resources: %w", err)
	}

	prefix := "ack-" + version + "-"
	result := make(map[string]string)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := gateResourcesFS.ReadFile("resources/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read gate file %s: %w", entry.Name(), err)
		}

		decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
		for {
			var cm corev1.ConfigMap
			if err := decoder.Decode(&cm); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, fmt.Errorf("failed to decode gate file %s: %w", entry.Name(), err)
			}

			for k, v := range cm.Data {
				if strings.HasPrefix(k, prefix) {
					result[k] = v
				}
			}
		}
	}

	return result, nil
}

// DiscoverGates lists ConfigMaps in the operator namespace that carry
// the upgrade-gate label and returns their merged data entries.
func (gc *GateChecker) DiscoverGates(ctx context.Context) (map[string]string, error) {
	var cmList corev1.ConfigMapList
	if err := gc.client.List(ctx, &cmList,
		client.InNamespace(gc.namespace),
		client.MatchingLabels{UpgradeGateLabel: "true"},
	); err != nil {
		return nil, fmt.Errorf("failed to list gate ConfigMaps: %w", err)
	}

	result := make(map[string]string)
	for i := range cmList.Items {
		maps.Copy(result, cmList.Items[i].Data)
	}

	return result, nil
}
