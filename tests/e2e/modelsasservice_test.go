package e2e_test

// ModelsAsService E2E tests are intentionally excluded from the CI suite.
//
// MaaS has no ODH component controller (NewComponentReconciler is a no-op);
// the maas-parameters ConfigMap is created by the DSC controller via
// AppendOperatorInstallManifests, which requires the full maas-controller
// manifest bundle on disk. The payload-processing-namespace fix is validated
// by the unit test TestMaasParametersConfigMapSetsPayloadProcessingNamespace
// in modelsasservice_test.go.
