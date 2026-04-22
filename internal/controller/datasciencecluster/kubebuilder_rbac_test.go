package datasciencecluster

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestEventsK8sIORBACMarkerIncludesCreate(t *testing.T) {
	data, err := os.ReadFile("kubebuilder_rbac.go")
	if err != nil {
		t.Fatalf("failed to read kubebuilder_rbac.go: %v", err)
	}

	re := regexp.MustCompile(`\+kubebuilder:rbac:groups="events\.k8s\.io",resources=events,verbs=([^\n]+)`)
	match := re.FindSubmatch(data)
	if match == nil {
		t.Fatal("no +kubebuilder:rbac marker found for events.k8s.io events resource")
	}

	verbs := string(match[1])
	for _, required := range []string{"create", "patch"} {
		found := false
		for _, v := range strings.Split(verbs, ";") {
			if v == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("events.k8s.io RBAC marker is missing the %q verb (got verbs=%s)", required, verbs)
		}
	}
}
