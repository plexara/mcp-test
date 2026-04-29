---
title: Installation
description: Install mcp-test from a release archive, the GHCR container image, go install, or by building from source.
---

# Installation

There are three ways to get mcp-test on your machine.

## 1. Download a release binary

Pre-built binaries for macOS (Intel + Apple Silicon), Linux (amd64 +
arm64), and Windows are attached to every tagged release on
[GitHub Releases](https://github.com/plexara/mcp-test/releases).

```bash
# macOS Apple Silicon
curl -L -o mcp-test.tar.gz \
  https://github.com/plexara/mcp-test/releases/latest/download/mcp-test_darwin_arm64.tar.gz
tar -xzf mcp-test.tar.gz
./mcp-test --version
```

Adjust the filename for your platform (`linux_amd64`, `linux_arm64`,
`darwin_amd64`, `windows_amd64`).

## 2. Pull the container image

The release pipeline publishes a multi-arch container image to GitHub
Container Registry on every tag and on `main` (as `:latest`).

```bash
docker pull ghcr.io/plexara/mcp-test:latest
docker run --rm ghcr.io/plexara/mcp-test:latest --version
```

The image runs as a non-root user (UID 1000) on a `scratch` base, so
it's small and has no shell. Mount your config file in:

```bash
docker run --rm \
  -p 8080:8080 \
  -e MCPTEST_DATABASE_URL=postgres://mcp:mcp@host.docker.internal:5432/mcp_test?sslmode=disable \
  -v $(pwd)/configs/mcp-test.yaml:/app/configs/mcp-test.yaml:ro \
  ghcr.io/plexara/mcp-test:latest --config /app/configs/mcp-test.yaml
```

## 3. Build from source

Requires Go 1.26.2 or later (and Node 22+ if you want the SPA bundled
in).

```bash
git clone https://github.com/plexara/mcp-test
cd mcp-test
make ui     # builds the React SPA into internal/ui/dist
make build  # produces ./bin/mcp-test
```

`make build` skips the SPA step; binaries built without the SPA serve
a small placeholder HTML at `/portal/` that links to the API. Run
`make ui` first if you need the full portal.

## Verify the build

```bash
./bin/mcp-test --version
# v0.0.0-dirty f294379 2026-04-29T03:08:54Z
```

Version comes from the latest git tag (`v0.0.0` if no tags exist) plus
a `-dirty` suffix when the working tree has uncommitted changes.
Pre-built release binaries always carry a clean version like `v1.2.3`.

## Next

- [Quickstart](quickstart.md) brings up Postgres + Keycloak in Docker
  and runs the binary against a working live config.
- [Configuration → YAML reference](../configuration/reference.md)
  covers every config key.
