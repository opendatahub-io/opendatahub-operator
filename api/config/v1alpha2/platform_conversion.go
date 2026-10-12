package v1alpha2

import "sigs.k8s.io/controller-runtime/pkg/conversion"

// Hub marks Platform v1alpha2 as the conversion hub.
func (*Platform) Hub() {}

var _ conversion.Hub = (*Platform)(nil)
