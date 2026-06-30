package errors

import (
	fwerrors "github.com/opendatahub-io/operator-actions-framework/controller/actions/errors"
)

type StopError = fwerrors.StopError
type RequeueAfterError = fwerrors.RequeueAfterError

var (
	NewStopError         = fwerrors.NewStopError
	NewStopErrorW        = fwerrors.NewStopErrorW
	NewRequeueAfterError = fwerrors.NewRequeueAfterError
)
