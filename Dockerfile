ARG ARCH=aarch64
ARG GO_VERSION=1.25.5
ARG SDK_VERSION=12.10.0
ARG SDK_REPO=axisecp

FROM --platform=linux/amd64 golang:${GO_VERSION} AS gobuilder
ARG ARCH
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY app ./app
RUN if [ "${ARCH}" = "aarch64" ]; then export GOARCH=arm64; else export GOARCH=arm GOARM=7; fi; \
    GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/netbird ./app

FROM ${SDK_REPO}/acap-native-sdk:${SDK_VERSION}-${ARCH}-ubuntu24.04 AS package
ARG ARCH
COPY --from=gobuilder /out/netbird /opt/app/netbird
COPY app /opt/app/
COPY native/param_bridge.c native/Makefile /opt/app/
COPY LICENSE /opt/app/LICENSE
RUN sed -i "s/\"BUILDARCH\"/\"${ARCH}\"/" /opt/app/manifest.json && \
    mkdir -p /opt/app/lib && cp /opt/app/netbird /opt/app/lib/netbird && \
    rm -f /opt/app/netbird && \
    chmod 755 /opt/app/NetBird_VPN_run /opt/app/lib/netbird && \
    . /opt/axis/acapsdk/environment-setup* && acap-build -a NetBird_VPN_run /opt/app

FROM scratch
COPY --from=package /opt/app/*eap /
