FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /app

COPY dist/transfer-continuous-worker ./server
RUN chmod +x ./server

ENV TZ=Asia/Shanghai

CMD ["./server"]
