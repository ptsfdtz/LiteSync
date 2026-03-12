FROM node:22-alpine AS web-builder
WORKDIR /web

RUN corepack enable

COPY client/package.json client/pnpm-lock.yaml client/pnpm-workspace.yaml client/turbo.json client/tsconfig.json ./
COPY client/apps/web/package.json ./apps/web/package.json
COPY client/packages/ui/package.json ./packages/ui/package.json

RUN pnpm install --frozen-lockfile

COPY client/. .

ARG VITE_API_BASE_URL=/api
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}

RUN pnpm --filter web build


FROM golang:1.25-alpine AS server-builder
WORKDIR /src

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/. .

RUN rm -rf internal/webui/dist
COPY --from=web-builder /web/apps/web/dist ./internal/webui/dist

RUN CGO_ENABLED=0 go build -o /out/litesync-server ./cmd/litesync-server


FROM alpine:3.22
WORKDIR /app

RUN adduser -D -h /app litesync
COPY --from=server-builder /out/litesync-server /usr/local/bin/litesync

RUN mkdir -p /data && chown -R litesync:litesync /data

USER litesync
ENV LITESYNC_HTTP_ADDR=:8080
ENV LITESYNC_DATA_DIR=/data

EXPOSE 8080
ENTRYPOINT ["litesync"]
