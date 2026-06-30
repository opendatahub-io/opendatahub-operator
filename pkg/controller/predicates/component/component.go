package component

import (
	fwlabel "github.com/opendatahub-io/operator-actions-framework/controller/predicates/label"
)

var (
	ForLabel          = fwlabel.ForLabel
	ForAnnotation     = fwlabel.ForAnnotation
	ForLabelAllEvents = fwlabel.ForLabelAllEvents
)
