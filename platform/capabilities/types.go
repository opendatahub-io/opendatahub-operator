package capabilities

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconciler is an interface that defines the capability management steps for given capability.
type Reconciler interface {
	IsRequired() bool
	Reconcile(ctx context.Context, cli client.Client, owner metav1.Object) error
}

type Availability interface {
	IsAvailable() bool
}

// PlatformCapabilities offers component views of the platform capabilities. This allows components to provide required
// information when onboarding.
type PlatformCapabilities interface {
	Authorization() Authorization
	Routing() Routing
}

func NewPlatformCapabilitiesStruct(p PlatformCapabilities) PlatformCapabilitiesStruct {
	return PlatformCapabilitiesStruct{p: p}
}

type PlatformCapabilitiesStruct struct {
	p PlatformCapabilities
}

func (p *PlatformCapabilitiesStruct) Authorization() Authorization {
	return p.p.Authorization()
}

func (p *PlatformCapabilitiesStruct) Routing() Routing {
	return p.p.Routing()
}

func (p *PlatformCapabilitiesStruct) DeepCopyInto(platform *PlatformCapabilitiesStruct) {

}

type InjectPlatformCapabilities interface {
	InjectPlatformCapabilities(platform PlatformCapabilitiesStruct)
}
