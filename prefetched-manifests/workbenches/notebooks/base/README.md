# IDE Imagestreams

Listing the order in which each imagestreams are introduced based on the `opendatahub.io/notebook-image-order` annotation in each file.

## Notebook Imagestreams (with order annotations):

1. jupyter-minimal-notebook-imagestream.yaml (Order: 1)
2. jupyter-minimal-gpu-notebook-imagestream.yaml (Order: 3)
3. jupyter-rocm-minimal-notebook-imagestream.yaml (Order: 5)
4. jupyter-datascience-notebook-imagestream.yaml (Order: 7)
5. jupyter-pytorch-notebook-imagestream.yaml (Order: 9)
6. jupyter-pytorch-llmcompressor-imagestream.yaml (Order: 10)
7. jupyter-rocm-pytorch-notebook-imagestream.yaml (Order: 12)
8. jupyter-tensorflow-notebook-imagestream.yaml (Order: 14)
9. jupyter-trustyai-notebook-imagestream.yaml (Order: 16)
10. jupyter-rocm-tensorflow-notebook-imagestream.yaml (Order: 16)
11. code-server-notebook-imagestream.yaml (Order: 19)

## Runtime Imagestreams (no order annotations):

- runtime-datascience-imagestream.yaml
- runtime-minimal-imagestream.yaml
- runtime-pytorch-imagestream.yaml
- runtime-rocm-pytorch-imagestream.yaml
- runtime-rocm-tensorflow-imagestream.yaml
- runtime-tensorflow-imagestream.yaml
- runtime-pytorch-llmcompressor-imagestream.yaml

The order is determined by the `opendatahub.io/notebook-image-order` annotation listed in each imagestream file.  
_Note_: On deprecation/removal of imagestream, the index of that image is retired with it.

## Params file

Please read workbench-naming for the name convention to follow in params.env.  
[Workbench Naming](../../docs/workbenches-naming.md)

- params-latest.env: This file contains references to latest versions of workbench images that are updated by konflux nudges.
- params.env: This file contains references to older versions of workbench images.

Image names follow the established IDE format:
`odh-<image type>-<image-feature>-<image-scope>-<accelerator>-<python-version>-<os-version>`
