package gates

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/blang/semver/v4"
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
// ConfigMap. Entries already set to "true" (admin-acknowledged) are
// never overwritten. Returns the list of unacknowledged gates.
//
// When called with an empty/nil map the ConfigMap is still created
// (if absent) to signal that gate evaluation has completed. Component
// controllers wait for the ConfigMap to exist before proceeding.
//
// The caller is responsible for assembling the correct set of gate
// entries: in-tree gates (unfiltered) and module/cluster gates
// (version-filtered). EnsureGates does not perform version filtering.
func (gc *GateChecker) EnsureGates(ctx context.Context, gateEntries map[string]string) ([]UnackedGate, error) {
	cm := &corev1.ConfigMap{}
	err := gc.client.Get(ctx, client.ObjectKey{
		Name:      AcksConfigMap,
		Namespace: gc.namespace,
	}, cm)

	if k8serr.IsNotFound(err) {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      AcksConfigMap,
				Namespace: gc.namespace,
				Annotations: map[string]string{
					ManagedByAnnotation: "opendatahub-operator",
				},
			},
			Data: make(map[string]string, len(gateEntries)),
		}
		for k, v := range gateEntries {
			cm.Data[k] = v
		}
		if createErr := gc.client.Create(ctx, cm); createErr != nil {
			if !k8serr.IsAlreadyExists(createErr) {
				return nil, fmt.Errorf("failed to create %s ConfigMap: %w", AcksConfigMap, createErr)
			}
			if err := gc.client.Get(ctx, client.ObjectKey{Name: AcksConfigMap, Namespace: gc.namespace}, cm); err != nil {
				return nil, fmt.Errorf("failed to get %s ConfigMap after race: %w", AcksConfigMap, err)
			}
		} else {
			return gc.collectUnacked(cm, gateEntries), nil
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get %s ConfigMap: %w", AcksConfigMap, err)
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string, len(gateEntries))
	}

	dirty := false
	for k, v := range gateEntries {
		if _, exists := cm.Data[k]; exists {
			continue
		}
		cm.Data[k] = v
		dirty = true
	}

	if dirty {
		if err := gc.client.Update(ctx, cm); err != nil {
			return nil, fmt.Errorf("failed to update %s ConfigMap: %w", AcksConfigMap, err)
		}
	}

	return gc.collectUnacked(cm, gateEntries), nil
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

// LoadInTreeGates reads all embedded YAML gate definitions from the
// resources/ directory. These gates are baked into the operator binary
// and always apply when upgrading from 2.x, regardless of the
// runtime-reported operator version (which can be incorrect during
// OLM's two-step CSV transition).
//
// Temporary: remove when all components are modules.
func LoadInTreeGates() (map[string]string, error) {
	entries, err := gateResourcesFS.ReadDir("resources")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded gate resources: %w", err)
	}

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
				result[k] = v
			}
		}
	}

	return result, nil
}

// MatchGateKey reports whether key applies to version and returns the gate
// name following the version scope. Supported key formats are:
//
//   - ack-<major>.<minor>-<gate-name>
//   - ack-<major>.<minor>.<patch>-<gate-name>
//
// Minor-scoped gates apply to every patch in that minor release. Exact gates
// apply only to the specified patch. Prerelease and build metadata in version
// do not affect matching.
func MatchGateKey(key string, version string) (string, bool) {
	const prefix = "ack-"

	if !strings.HasPrefix(key, prefix) {
		return "", false
	}

	current, err := semver.Parse(version)
	if err != nil {
		return "", false
	}

	remainder := strings.TrimPrefix(key, prefix)
	separator := strings.IndexByte(remainder, '-')
	if separator <= 0 || separator == len(remainder)-1 {
		return "", false
	}

	target, ok := parseGateTarget(remainder[:separator])
	if !ok || target.major != current.Major || target.minor != current.Minor {
		return "", false
	}
	if target.hasPatch && target.patch != current.Patch {
		return "", false
	}

	return remainder[separator+1:], true
}

type versionCore struct {
	major    uint64
	minor    uint64
	patch    uint64
	hasPatch bool
}

func parseGateTarget(value string) (versionCore, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return versionCore{}, false
	}

	result := versionCore{hasPatch: len(parts) == 3}
	normalized := value
	if len(parts) == 2 {
		normalized += ".0"
	}

	parsed, err := semver.Parse(normalized)
	if err != nil || len(parsed.Pre) > 0 || len(parsed.Build) > 0 {
		return versionCore{}, false
	}

	result.major = parsed.Major
	result.minor = parsed.Minor
	result.patch = parsed.Patch

	return result, true
}

// AllGatesAcknowledged returns true when the odh-upgrade-acks
// ConfigMap exists and every gate entry matching one of the target versions is
// set to "true". Entries for other versions are ignored. Returns false when
// the ConfigMap does not exist (the modules controller has not yet evaluated
// gates) or when an applicable entry remains unacknowledged. An empty or
// out-of-scope ConfigMap is considered fully acknowledged.
func AllGatesAcknowledged(ctx context.Context, cli client.Client, namespace string, versions []string) (bool, error) {
	cm := &corev1.ConfigMap{}
	if err := cli.Get(ctx, client.ObjectKey{Name: AcksConfigMap, Namespace: namespace}, cm); err != nil {
		if k8serr.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get %s ConfigMap: %w", AcksConfigMap, err)
	}

	for key, val := range cm.Data {
		if !matchesAnyVersion(key, versions) {
			continue
		}
		if val != "true" {
			return false, nil
		}
	}

	return true, nil
}

func matchesAnyVersion(key string, versions []string) bool {
	for _, version := range versions {
		if _, matches := MatchGateKey(key, version); matches {
			return true
		}
	}
	return false
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
		for k, v := range cmList.Items[i].Data {
			result[k] = v
		}
	}

	return result, nil
}
