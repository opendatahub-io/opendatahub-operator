ARG binary_name=kfctl
FROM docker.io/openshift/origin-release:golang-1.13 AS build
ARG binary_name

COPY . /opt/kfctl
WORKDIR /opt/kfctl/

RUN GOOS=linux GOARCH=amd64 go build --trimpath -a -o build/_output/bin/${binary_name} -ldflags="-X 'github.com/kubeflow/kfctl/v3/pkg/kfapp/kustomize.enableKustAlphaPlugin=yes'" cmd/manager/main.go
RUN cd kustomize-fns/v1alpha1/applyresources && go mod vendor && \
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build --trimpath --mod=vendor --buildmode=plugin -o /opt/kfctl/build/_output/plugins/v1alpha1/applyresources/ApplyResources.so ApplyResources.go

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ARG binary_name
ENV OPERATOR=/usr/local/bin/${binary_name}\
    HOME=/opt/${binary_name}

RUN mkdir -p ${HOME} &&\
    chown 1001:0 ${HOME} &&\
    chmod ug+rwx ${HOME}

WORKDIR ${HOME}

COPY --from=build /opt/kfctl/build/_output/bin/${binary_name} ${OPERATOR}
COPY --from=build /opt/kfctl/build/_output/plugins/ $HOME/.config/kustomize/plugin/

ENTRYPOINT ["${OPERATOR}"]

USER 1001
