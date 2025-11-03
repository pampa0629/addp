# Transfer Backend Dockerfile (prebuilt binary)

FROM alpine:latest

WORKDIR /app

# Install curl for health checks
RUN apk add --no-cache curl ca-certificates

# Copy precompiled binary produced by scripts/deploy/0-compile-binaries.sh
# or `make build` to dist, then move/copy to transfer/backend/server
COPY transfer/backend/server ./server

EXPOSE 8083

CMD ["./server"]
