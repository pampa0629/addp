FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata curl
WORKDIR /app
COPY dist/quality-backend ./server
RUN chmod +x ./server
ENV TZ=Asia/Shanghai
EXPOSE 8182
CMD ["./server"]
