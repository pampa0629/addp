FROM golang:1.23-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /app

RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/server .

EXPOSE 8083

CMD ["./server"]
