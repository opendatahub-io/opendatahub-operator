package manager

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
)

type gvkInfo struct {
	owned bool
}

func New(manager ctrl.Manager) *Manager {
	return &Manager{
		m:    manager,
		gvks: map[schema.GroupVersionKind]gvkInfo{},
	}
}

type Manager struct {
	m ctrl.Manager

	gvks map[schema.GroupVersionKind]gvkInfo
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
