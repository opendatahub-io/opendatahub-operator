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

// PlatformCapabilitiesStruct is a wrapper struct to inject PlatformCapabilities into components instead of polluting
// the interface.
//
// We cannot directly use an interface type, so this struct wrapper exists to work around the hard requirement that the implementation
// of the component is an API-facing concept. Without this indirection, the API generation toolchain fails to generate the deep copy.
type PlatformCapabilitiesStruct struct {
	p PlatformCapabilities
}

func (p *PlatformCapabilitiesStruct) Authorization() Authorization {
	return p.p.Authorization()
}

func (p *PlatformCapabilitiesStruct) Routing() Routing {
	return p.p.Routing()
}

func (p *PlatformCapabilitiesStruct) DeepCopyInto(_ *PlatformCapabilitiesStruct) {

}

type Injectable interface {
	InjectPlatformCapabilities(platform PlatformCapabilitiesStruct)
}
