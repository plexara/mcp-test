# --- node stage: build the React SPA into internal/ui/dist
FROM node:22-alpine AS ui
WORKDIR /src
COPY ui/package.json ui/pnpm-lock.yaml* ui/package-lock.json* ./ui/
RUN if [ -f ui/pnpm-lock.yaml ]; then \
      corepack enable && cd ui && pnpm install --frozen-lockfile; \
    elif [ -f ui/package-lock.json ]; then \
      cd ui && npm ci; \
    else \
      cd ui && npm install; \
    fi
COPY ui ./ui
RUN cd ui && (pnpm build 2>/dev/null || npm run build)

# --- go stage: build the binary with the SPA embedded
FROM golang:1.26.2-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/ui/dist /src/internal/ui/dist
ARG VERSION=dev
ARG GIT_SHA=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w \
      -X github.com/plexara/mcp-test/pkg/build.Version=${VERSION} \
      -X github.com/plexara/mcp-test/pkg/build.Commit=${GIT_SHA} \
      -X github.com/plexara/mcp-test/pkg/build.Date=${BUILD_DATE}" \
    -o /out/mcp-test ./cmd/mcp-test

# --- runtime stage: distroless static, non-root
FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/mcp-test /usr/local/bin/mcp-test
COPY configs/mcp-test.example.yaml /app/configs/mcp-test.yaml
COPY configs/mcp-test.dev.yaml      /app/configs/mcp-test.dev.yaml
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mcp-test"]
