FROM registry.redhat.io/ubi9/go-toolset:1.25 AS builder

COPY . /workspace
WORKDIR /workspace

ARG BUILD_VERSION=v0.0.0
ARG SOURCE_GIT_COMMIT=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN set -e && \
    CRANE_VERSION="${BUILD_VERSION:-dev}" && \
    CRANE_GIT_COMMIT="${SOURCE_GIT_COMMIT:-unknown}" && \
    mkdir -p /tmp/archives /tmp/bin && \
    for platform in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
        os=$(echo "$platform" | cut -d'/' -f1) && \
        arch=$(echo "$platform" | cut -d'/' -f2) && \
        if [ "$os" = "windows" ]; then \
            output="crane_${os}_${arch}.exe"; \
        else \
            output="crane_${os}_${arch}"; \
        fi && \
        echo "Building standalone binary $output (version=${CRANE_VERSION}, commit=${CRANE_GIT_COMMIT})..." && \
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -mod=readonly -a \
            -ldflags="-X github.com/konveyor/crane/internal/buildinfo.Version=${CRANE_VERSION}" \
            -o "/tmp/archives/$output" ./main.go && \
        (cd /tmp/archives && sha256sum "$output" > "$output.sha256"); \
    done && \
    cp LICENSE /tmp/archives/LICENSE

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -mod=readonly -a \
    -ldflags="-X github.com/konveyor/crane/internal/buildinfo.Version=${BUILD_VERSION}" \
    -o /tmp/bin/crane ./main.go

FROM registry.access.redhat.com/ubi9:latest

RUN dnf -y install openssl && dnf -y reinstall tzdata && dnf clean all

COPY --from=builder /tmp/archives /archives
COPY --from=builder /tmp/bin/crane /usr/local/bin/crane
COPY LICENSE /licenses/

USER 1001

ENTRYPOINT ["/usr/local/bin/crane"]

LABEL \
    description="Crane CLI for Kubernetes migration workflows" \
    io.k8s.description="Crane CLI for Kubernetes migration workflows" \
    io.k8s.display-name="Crane CLI" \
    io.openshift.maintainer.project="Crane" \
    io.openshift.tags="migration,modernization,konveyor,crane" \
    summary="Crane CLI"
