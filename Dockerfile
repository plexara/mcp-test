# syntax=docker/dockerfile:1
#
# mcp-test runtime image. Goreleaser supplies the pre-built binary in
# the build context (one per linux/<arch>); we just bundle it with CA
# certs and run as a non-root user.

FROM alpine:3.23 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch

# TLS root certs so OIDC discovery (HTTPS to the IdP) works.
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Goreleaser sets TARGETARCH for the platform-specific binary path.
ARG TARGETARCH
COPY linux/${TARGETARCH}/mcp-test /usr/local/bin/mcp-test

# Bundle the example config so a docker run without a mounted config
# still has something to point at.
COPY configs/mcp-test.example.yaml /app/configs/mcp-test.yaml

# Non-root (scratch has no /etc/passwd; numeric IDs only).
USER 1000:1000

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/mcp-test"]
CMD ["--config", "/app/configs/mcp-test.yaml"]
