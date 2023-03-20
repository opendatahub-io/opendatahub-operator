## v1.5.0

### Changes

- Bumped operator-sdk to v1.24.2([#176](https://github.com/opendatahub-io/opendatahub-operator/issues/176))
- Bumped Go version to v1.18
- Bumped [k8s.io](https://pkg.go.dev/k8s.io/kubectl@v0.21.0) dependencies to v0.21 

### Additions

- Added steps to build and deploy Operator using custom OLM bundle.([#184](https://github.com/opendatahub-io/opendatahub-operator/pull/184/commits/00d5a07e8580529e5b6187fb970ab7bf7e0028fb))
- Added support for scoredcard tests provided by the operator-sdk([#189](https://github.com/opendatahub-io/opendatahub-operator/issues/189))
- Added e2e testing framework.([#201](https://github.com/opendatahub-io/opendatahub-operator/pull/201))
- Added OwnerReferences to Kfdef ([#209](https://github.com/opendatahub-io/opendatahub-operator/issues/209))
- Added support for `opendatahub.io/configurable=true` label. Manifests owners can add this label to make a resource configurable.([#208](https://github.com/opendatahub-io/opendatahub-operator/issues/208))

### Bug Fixes
- Remove deprecated methods after version bump ([#184](https://github.com/opendatahub-io/opendatahub-operator/pull/184))
- Remove support for v1alpha1 and v1beta1 of KfDef ([#184](https://github.com/opendatahub-io/opendatahub-operator/pull/184))
