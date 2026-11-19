# An optional all-in-one console for sidecars. Production's split images remain available.
FROM --platform=$BUILDPLATFORM node:23-alpine AS console
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
WORKDIR /app
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS gateway
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG VERSION=unknown
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" -o /out/avior ./cmd/avior
RUN CGO_ENABLED=0 GOARCH=${TARGETARCH} go install github.com/pressly/goose/v3/cmd/goose@v3.22.1 && \
    find /go/bin -type f -name goose -exec cp {} /out/goose \;

FROM alpine:3.21
RUN apk add --no-cache ca-certificates postgresql17-client && adduser -D -u 65532 avior
COPY --from=gateway /out/avior /usr/local/bin/avior
COPY --from=gateway /out/goose /usr/local/bin/goose
COPY --from=console /app/dist /ui
COPY db/migrations /migrations
COPY deploy/standalone/ /standalone/
ENV AVIOR_UI_DIR=/ui
USER avior
EXPOSE 8080
ENTRYPOINT ["/bin/sh", "/standalone/run.sh"]
CMD ["serve"]
