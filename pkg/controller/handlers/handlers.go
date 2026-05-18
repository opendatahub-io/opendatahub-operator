package handlers

import (
	fwhandlers "github.com/opendatahub-io/operator-actions-framework/controller/handlers"
)

var (
	LabelToName       = fwhandlers.LabelToName
	AnnotationToName  = fwhandlers.AnnotationToName
	Fn                = fwhandlers.Fn
	ToNamed           = fwhandlers.ToNamed
	RequestFromObject = fwhandlers.RequestFromObject
)
