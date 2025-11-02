# Transfer Backend Dockerfile
# Uses precompiled binary from bin/transfer-backend

FROM alpine:latest

WORKDIR /app

# Install curl for health checks
RUN apk add --no-cache curl ca-certificates

# Copy precompiled binary
COPY ../../bin/transfer-backend ./server

EXPOSE 8083

CMD ["./server"]
