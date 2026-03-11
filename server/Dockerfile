FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN go build -o /out/litesync-server ./cmd/litesync-server

FROM alpine:3.22
WORKDIR /app

RUN adduser -D -h /app litesync
COPY --from=builder /out/litesync-server /usr/local/bin/litesync-server

RUN mkdir -p /data && chown -R litesync:litesync /data

USER litesync
ENV LITESYNC_HTTP_ADDR=:8080
ENV LITESYNC_DATA_DIR=/data

EXPOSE 8080
ENTRYPOINT ["litesync-server"]

