# rhcl-operator

![Version: 1.0.0](https://img.shields.io/badge/Version-1.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.3.0](https://img.shields.io/badge/AppVersion-1.3.0-informational?style=flat-square)

Red Hat Connectivity Link (Kuadrant) operators for vanilla Kubernetes (without OLM)

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Red Hat |  |  |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| bundle.version | string | `"1.3.0"` |  |
| imagePullSecret.dockerConfigJson | string | `""` |  |
| imagePullSecret.name | string | `"rhai-pull-secret"` |  |
| imagePullSecrets[0].name | string | `"rhai-pull-secret"` |  |
| operandNamespace | string | `"kuadrant-system"` |  |
| operatorNamespace | string | `"kuadrant-operators"` |  |

