# cert-manager-operator

![Version: 1.1.0](https://img.shields.io/badge/Version-1.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.18.1](https://img.shields.io/badge/AppVersion-1.18.1-informational?style=flat-square)

Red Hat cert-manager Operator for vanilla Kubernetes (without OLM)

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Red Hat |  |  |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| bundle.version | string | `"v1.18.1"` |  |
| imagePullSecrets[0].name | string | `"rhai-pull-secret"` |  |
| operandNamespace | string | `"cert-manager"` |  |
| operatorNamespace | string | `"cert-manager-operator"` |  |

