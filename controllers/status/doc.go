// Package status provides a generic way to report status and conditions for any resource of type client.Object.
//
// The Reporter struct centralizes the management of status and condition updates for any resource type that implements client.Object.
// This approach consolidates the previously scattered updateStatus functions found in DSCI and DSC controllers into a single,
// reusable component.
//
// Reporter handles the reporting of a resource's condition based on the operational state and errors encountered during processing.
// It uses a closure, DetermineCondition, defined by the developer to determine how conditions should be updated,
// particularly in response to errors.
// This closure is similar to its previous incarnation "update func(saved)", which appends the target object's
// conditions, with the only difference being access to an optional error to make changes in the condition
// to be reported based on the occurred error.
//
// Example:
//
// createReporter initializes a new status reporter for a DSCInitialization resource.
// It encapsulates the logic for updating the condition based on errors encountered during the resource's lifecycle operations.
//
//	func createReporter(cli client.Client, object *dsciv1.DSCInitialization, condition *conditionsv1.Condition) *status.Reporter[*dsciv1.DSCInitialization] {
//		return status.NewStatusReporter[*dsciv1.DSCInitialization](
//			cli,
//			object,
//			func(err error) status.SaveStatusFunc[*dsciv1.DSCInitialization] {
//				return func(saved *dsciv1.DSCInitialization) {
//					if err != nil {
//						condition.Status = corev1.ConditionFalse
//						condition.Message = err.Error()
//						condition.Reason = status.CapabilityFailed
//						var missingOperatorErr *feature.MissingOperatorError
//						if errors.As(err, &missingOperatorErr) {
//							condition.Reason = status.MissingOperatorReason
//						}
//					}
//					conditionsv1.SetStatusCondition(&saved.Status.Conditions, *condition)
//				}
//			},
//		)
//	}
//
// doServiceMeshStuff manages the Service Mesh configuration process during DSCInitialization reconcile.
// It creates a reporter and reports any conditions derived from the service mesh configuration process.
//
//	func (r *DSCInitializationReconciler) doStdoServiceMeshStuffff(instance *dsciv1.DSCInitialization) error {
//		reporter := createReporter(r.Client, instance, &conditionsv1.Condition{
//			Type:    status.CapabilityServiceMesh,
//			Status:  corev1.ConditionTrue,
//			Reason:  status.ConfiguredReason,
//			Message: "Service Mesh configured",
//		})
//
//		serviceMeshErr := createServiceMesh(instance)
//		_, reportError := reporter.ReportCondition(serviceMeshErr)
//
//		return multierror.Append(serviceMeshErr, reportError) // return all errors
//	}
package status
