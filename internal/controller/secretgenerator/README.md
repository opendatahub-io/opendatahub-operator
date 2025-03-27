# Secrets generator controller

Uploading secrets in plain text to git repositories is a common security issue
in public repositories. Kustomize doesn't have a proper way of generating
secrets on-demand, this controller adds the capability of generating random
secrets in Openshift that can be used by other apps.

## Basic usage

Create a Kubernetes secret with the `secret-generator.opendatahub.io/name`
annotation, for example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: example
  annotations:
    secret-generator.opendatahub.io/name: "password"
    secret-generator.opendatahub.io/type: "random"
    secret-generator.opendatahub.io/complexity: "16"
type: Opaque
```

The controller will generate a new secret, with the same name and appending the
suffix `-generated`, including the generated random value in the `.data` field:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: example-generated
data:
  password: jgKGv6grDaLEMo6r
type: Opaque
```

## Secret types

Generate different secret types based on the
`secret-generator.opendatahub.io/type` annotation:

- **random**: Generate a random string of the length specified in the complexity
  annotation. For example, `jgKGv6grDaLEMo6r` (complexity 16).
- **oauth**: Generate an OAuth cookie secret. For example
  `dURVM2VrQVI5cnZmK0ZkZXFsNDQrdz09` (complexity 16).
