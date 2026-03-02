FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata curl
WORKDIR /app
COPY dist/portal-backend ./server
RUN chmod +x ./server
ENV TZ=Asia/Shanghai
EXPOSE 8184
CMD ["./server"]
