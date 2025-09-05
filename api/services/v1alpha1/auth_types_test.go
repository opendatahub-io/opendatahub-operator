package v1alpha1

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestAuthSpecStructure validates the basic Go struct functionality of AuthSpec.
// This test ensures:
//
// 1. AdminGroups and AllowedGroups arrays can be properly initialized and accessed
// 2. Field assignment and retrieval work correctly
// 3. The struct can handle various group configurations including system:authenticated
//
// Note: This test validates Go-level functionality only. Security validation (preventing
// system:authenticated in AdminGroups) is handled by CEL validation rules and tested
// separately in auth_cel_integration_test.go
func TestAuthSpecStructure(t *testing.T) {
	g := NewWithT(t)

	spec := AuthSpec{
		AdminGroups:   []string{"admin1", "admin2"},
		AllowedGroups: []string{"user1", "user2", "system:authenticated"},
	}

	g.Expect(spec.AdminGroups).To(HaveLen(2))
	g.Expect(spec.AdminGroups).To(ContainElements("admin1", "admin2"))

	g.Expect(spec.AllowedGroups).To(HaveLen(3))
	g.Expect(spec.AllowedGroups).To(ContainElements("user1", "user2", "system:authenticated"))
}

// TestAuthList validates that the AuthList type works correctly for API collection operations.
// This test ensures:
//
// 1. Multiple Auth objects can be stored in a list
// 2. Individual items in the list are accessible and properly structured
// 3. List operations work as expected for API consumers
//
// While Auth is typically a singleton resource (only one instance named "auth" should exist),
// the AuthList type is still required by the Kubernetes API machinery for list operations
// and API generation.
func TestAuthList(t *testing.T) {
	g := NewWithT(t)

	authList := AuthList{
		Items: []Auth{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "auth1"},
				Spec: AuthSpec{
					AdminGroups:   []string{"admin1"},
					AllowedGroups: []string{"user1"},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "auth2"},
				Spec: AuthSpec{
					AdminGroups:   []string{"admin2"},
					AllowedGroups: []string{"user2"},
				},
			},
		},
	}

	g.Expect(authList.Items).To(HaveLen(2))
	g.Expect(authList.Items[0].Name).To(Equal("auth1"))
	g.Expect(authList.Items[1].Name).To(Equal("auth2"))
}
