We are disabling this until we can ensure compatibility with ODH Nithlies.

To test the modular architecture, you can use the following steps:

1. Modify the `manifests/odh/kustomization.yaml` file to include the `../modular-architecture` path.

```
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
commonLabels:
  app: odh-dashboard
  app.kubernetes.io/part-of: odh-dashboard
resources:
  - ../common
  - ../modular-architecture
  - ../core-bases/consolelink
configMapGenerator:
  - name: odh-dashboard-params
    env: params.env
```

2. Deploy to the cluster with the command `kubectl apply -k manifests/odh`.
