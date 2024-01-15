## Upgrade testing 
Follow below step for manual upgrade testing 

1. Set environment variables to overwrite values in Makefile

```
IMAGE_OWNER ?= opendatahub
VERSION ?= 2.4.0
```

2. Add `replaces` proprty in `opendatahub-operator.clusterserviceversion.yaml` file under `config/manifests/bases` and add version which you would like to upgrade with next version

```
replaces: opendatahub-operator.v2.4.0
```

3. Build and push docker container image

```
make image
```

4. Build bundle image 

```
make bundle-build
```

5. Push bundle image into registry

```
make bundle-push
```

6. Build catalog source image 

```
make catalog-build
```

7. Push catalog source image into registry

```
make catalog-push
```
### Cluster

8. Create catalog source on cluster by using your catalog source container image and wait until catalog source pod is ready

9. Go to OperatorHub page and find catalog source name in `Source` section and select catalog source. Install `Open Data Hub Operator`. Wait until operator is ready.

10. Follow steps `1 to 7` for upgrade version

11. Update catalog source with new catalog source container image on cluster

12. Go to Installed Operator `Open Data Hub Operator` and upgrade operator with latest version under `Subscription` section on cluster


