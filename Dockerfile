# 跨平台构建镜像。
#
# 注意：DPAPI 只有 Windows 有，容器里无法自动读取 CodeArts Agent 的 secret storage。
# 在容器里请通过控制台「手工录入」或「从 session JSON 导入」提供凭据
# （session JSON 含 refresh_token 与 DPoP 私钥，因此仍可自动续期）。
# UI assets are architecture independent; run checks natively once, including
# when the target image is ARM. Avoid executing the test runner under QEMU.
FROM --platform=$BUILDPLATFORM node:24.11.0-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN npm install --global pnpm@10.34.9 && pnpm install --frozen-lockfile
COPY frontend ./
COPY api /src/api
COPY scripts/teleagent-login.mjs /src/scripts/teleagent-login.mjs
RUN pnpm typecheck && pnpm test && pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
WORKDIR /src
COPY backend/go.mod ./
COPY backend/cmd ./cmd
COPY backend/internal ./internal
COPY --from=frontend /src/backend/internal/webui/dist ./internal/webui/dist
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/codearts2api ./cmd/panel

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -u 10001 codearts && \
    mkdir -p /data && chown 10001:10001 /data
COPY --from=build /out/codearts2api /usr/local/bin/codearts2api
COPY deploy/healthcheck.sh /usr/local/bin/codearts-healthcheck
COPY LICENSE NOTICE LICENSE.workbuddy LICENSE.qoder LICENSE.kiro LICENSE.teleagent /usr/share/licenses/codearts2api/
COPY backend/internal/catalog/modelmeta/LICENSE backend/internal/catalog/modelmeta/COPYING backend/internal/catalog/modelmeta/UPSTREAM.md backend/internal/catalog/modelmeta/upstream.json /usr/share/licenses/codearts2api/modelmeta/
COPY backend/internal/responsescompat/LICENSE backend/internal/responsescompat/COPYING backend/internal/responsescompat/UPSTREAM.md /usr/share/licenses/codearts2api/responsescompat/
USER 10001:10001
WORKDIR /data
VOLUME ["/data"]
EXPOSE 10087
HEALTHCHECK --interval=30s --timeout=10s --start-period=15s --retries=3 CMD sh /usr/local/bin/codearts-healthcheck
ENTRYPOINT ["codearts2api"]
CMD ["-host", "0.0.0.0", "-port", "10087", "-data", "/data/codearts2api-data.json"]
