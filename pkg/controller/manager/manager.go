package manager

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
)

type gvkInfo struct {
	owned bool
}

func New(manager ctrl.Manager, ownerType *schema.GroupVersionKind) *Manager {
	return &Manager{
		m:         manager,
		ownerType: ownerType,
		gvks:      map[schema.GroupVersionKind]gvkInfo{},
	}
}

type Manager struct {
	m ctrl.Manager

	ownerType *schema.GroupVersionKind
	gvks      map[schema.GroupVersionKind]gvkInfo
}

func (m *Manager) GetOwnerType() *schema.GroupVersionKind {
	return m.ownerType
}

func (m *Manager) AddGVK(gvk schema.GroupVersionKind, owned bool) {
	if m == nil {
		return
	}

	m.gvks[gvk] = gvkInfo{
		owned: owned,
	}
}

func (m *Manager) Owns(gvk schema.GroupVersionKind) bool {
	if m == nil {
		return false
	}

	i, ok := m.gvks[gvk]
	return ok && i.owned
}
