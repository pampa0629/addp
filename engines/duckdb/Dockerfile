ARG GO_IMAGE=localhost:5001/golang:1.24-bookworm
ARG RUNTIME_IMAGE=localhost:5001/debian-slim:bookworm

FROM ${GO_IMAGE} AS builder

WORKDIR /workspace

COPY common/go.mod common/go.sum ./common/
COPY engines/duckdb/go.mod engines/duckdb/go.sum ./engines/duckdb/

WORKDIR /workspace/engines/duckdb
ENV GOWORK=off
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
ARG TARGETOS
ARG TARGETARCH
RUN go mod download

WORKDIR /workspace
COPY common ./common
COPY engines/duckdb ./engines/duckdb

WORKDIR /workspace/engines/duckdb
RUN apt-get update && apt-get install -y --no-install-recommends cmake ninja-build bison flex patch python3 libssl-dev && rm -rf /var/lib/apt/lists/*
RUN --mount=type=cache,id=duckdb-scanner-${TARGETOS}-${TARGETARCH},target=/native-build \
    digest=$(./postgres-scanner/build.sh /native-build /out/extensions) \
    && flags="-s -w -X github.com/addp/engines/duckdb/internal/duckdb.postgresScannerSHA256=$digest" \
    && CGO_ENABLED=1 go build -ldflags="$flags" -o /out/addp-duckdb ./cmd/server \
    && CGO_ENABLED=1 go build -ldflags="$flags" -o /out/prepare-extensions ./cmd/prepare-extensions
RUN --mount=type=cache,id=duckdb-extensions-${TARGETOS}-${TARGETARCH}-v1.4.5,target=/extension-cache \
    /out/prepare-extensions --output /extension-cache --verify=false \
    && cp /extension-cache/httpfs.duckdb_extension /extension-cache/mysql_scanner.duckdb_extension /extension-cache/spatial.duckdb_extension /out/extensions/ \
    && /out/prepare-extensions --output /out/extensions

FROM ${RUNTIME_IMAGE}

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/addp-duckdb /usr/local/bin/addp-duckdb
COPY --from=builder /out/extensions /opt/addp/duckdb/extensions

ENV DUCKDB_RUNTIME_PORT=8104 \
    DUCKDB_EXTENSION_DIRECTORY=/opt/addp/duckdb/extensions

EXPOSE 8104

CMD ["/usr/local/bin/addp-duckdb"]
