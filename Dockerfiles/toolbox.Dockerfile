FROM registry.fedoraproject.org/fedora-toolbox:38

ARG GOLANG_VERSION=1.23.8
ARG OPERATOR_SDK_VERSION=1.31.0

ENV GOLANG_VERSION=$GOLANG_VERSION \
    OPERATOR_SDK_VERSION=$OPERATOR_SDK_VERSION

RUN curl -Lo /tmp/golang.tgz https://go.dev/dl/go${GOLANG_VERSION}.linux-amd64.tar.gz \
 && tar xvzf /tmp/golang.tgz -C /usr/local \
 && curl -Lo /usr/local/bin/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_linux_amd64 \
 && echo -e '#!/bin/bash\nflatpak-spawn --host podman "${@}"' > /usr/local/bin/podman \
 && chmod +x /usr/local/bin/operator-sdk /usr/local/bin/podman \
 && echo -e 'GOROOT=/usr/local/go\nGOPATH=$HOME/go\nPATH=$GOPATH/bin:$GOROOT/bin:$PATH\nexport GOROOT GOPATH PATH' > /etc/profile.d/go.sh \
 && dnf -y install gcc make ripgrep \
 && dnf -y clean all
