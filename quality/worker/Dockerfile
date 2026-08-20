FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY dist/quality-worker ./server
RUN chmod +x ./server

ENV TZ=Asia/Shanghai

CMD ["./server"]
