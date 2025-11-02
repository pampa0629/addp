FROM golang:1.23-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /workspace

RUN apk add --no-cache gcc musl-dev

COPY common ./common
COPY meta/backend/go.mod meta/backend/go.sum ./meta/backend/

WORKDIR /workspace/meta/backend

RUN go mod download

COPY meta/backend ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /workspace/server ./cmd/server

FROM alpine:latest

WORKDIR /app

COPY --from=builder /workspace/server .

EXPOSE 8082

CMD ["./server"]
