FROM registry.redhat.io/ubi9/go-toolset:1.25 AS builder

COPY . /workspace
WORKDIR /workspace

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN set -e && \
    # Prefer ART/Konflux injected env vars; fall back for local builds.
    MTA_OPS_VERSION="${BUILD_VERSION:-dev}" && \
    MTA_OPS_GIT_COMMIT="${SOURCE_GIT_COMMIT:-unknown}" && \
    mkdir -p /tmp/archives /tmp/bin && \
    for platform in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
        os=$(echo "$platform" | cut -d'/' -f1) && \
        arch=$(echo "$platform" | cut -d'/' -f2) && \
        if [ "$os" = "windows" ]; then \
            output="mta-ops_${os}_${arch}.exe"; \
        else \
            output="mta-ops_${os}_${arch}"; \
        fi && \
        echo "Building standalone binary $output (version=${MTA_OPS_VERSION}, commit=${MTA_OPS_GIT_COMMIT})..." && \
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -mod=readonly \
            -ldflags="-X github.com/konveyor/crane/internal/buildinfo.Version=${MTA_OPS_VERSION}" \
            -o "/tmp/archives/$output" ./main.go && \
        (cd /tmp/archives && sha256sum "$output" > "$output.sha256"); \
    done && \
    cp LICENSE /tmp/archives/LICENSE && \
    go clean -cache -modcache -testcache && \
    rm -rf /opt/app-root/src/.cache /opt/app-root/src/go/pkg /tmp/go /tmp/.cache

RUN MTA_OPS_VERSION="${BUILD_VERSION:-dev}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -mod=readonly \
    -ldflags="-X github.com/konveyor/crane/internal/buildinfo.Version=${MTA_OPS_VERSION}" \
    -o /tmp/bin/mta-ops ./main.go

FROM registry.access.redhat.com/ubi9:latest

RUN dnf -y install openssl && dnf -y reinstall tzdata && dnf clean all

COPY --from=builder /tmp/archives /archives
COPY --from=builder /tmp/bin/mta-ops /usr/local/bin/mta-ops
COPY LICENSE /licenses/

USER 1001

ENTRYPOINT ["/usr/local/bin/mta-ops"]

LABEL \
    description="MTA-Ops CLI for Kubernetes migration workflows" \
    io.k8s.description="MTA-Ops CLI for Kubernetes migration workflows" \
    io.k8s.display-name="MTA-Ops CLI" \
    io.openshift.maintainer.project="MTA-Ops" \
    io.openshift.tags="migration,modernization,konveyor,mta-ops" \
    summary="MTA-Ops CLI"
