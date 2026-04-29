# Releases

mcp-test follows semantic versioning. Every push to `main` is
verified by CI; every git tag matching `v*` triggers a release.

## Where to find a release

- **Binaries** for macOS, Linux, and Windows are attached to each
  release on
  [GitHub Releases](https://github.com/plexara/mcp-test/releases),
  bundled with `LICENSE` and `README.md`.
- **Container images** are published to
  [GitHub Container Registry](https://github.com/plexara/mcp-test/pkgs/container/mcp-test)
  with three tags: `latest`, `vX.Y.Z`, `vX`.

## Cutting a release

```bash
# Make sure main is green.
git checkout main && git pull
git status   # clean

# Tag and push.
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

GitHub Actions (`.github/workflows/release.yml`) takes over from
there:

1. Sets up Go and Buildx, logs into GHCR.
2. Runs `goreleaser release --clean`:
    - Cross-compiles binaries for `linux/amd64`, `linux/arm64`,
      `darwin/amd64`, `darwin/arm64`, `windows/amd64`.
    - Builds a multi-arch container image and pushes to
      `ghcr.io/plexara/mcp-test:vX.Y.Z`, `:vX`, `:latest`.
    - Generates a GitHub Release with a generated changelog
      grouped by `feat:`, `fix:`, `security:`, others.
    - Attaches all binary archives plus a `checksums.txt`.

The whole pipeline takes ~3-5 minutes.

## Versioning policy

- **Major** (`v1`, `v2`): breaking change to the config schema, the
  HTTP API, the audit-row structure, or the tool input/output
  shapes. The deterministic-output guarantees on `fixed_response`,
  `lorem`, and `flaky` may change at major boundaries; patch- and
  minor-version bumps preserve them.
- **Minor** (`v1.1`, `v1.2`): new tools, new portal pages, new
  optional config fields. Existing configs and tests stay valid.
- **Patch** (`v1.0.1`): bug fixes, dependency bumps, doc-only
  changes.

`v0.x.y` is a different beast: until we hit `v1.0.0`, the schema and
APIs may change between minor versions. Pin a specific tag in
production.

## Verifying a release

For binaries:

```bash
sha256sum -c checksums.txt
```

For images, the registry stores the digest in the manifest. Pin in
production deployments to a digest, not a tag:

```bash
docker pull ghcr.io/plexara/mcp-test@sha256:<digest>
```

(We do not currently sign with cosign or generate SBOMs; both are
straightforward to add and we'd take a PR doing so.)

## Changelog

Each GitHub Release page carries a generated changelog grouped by
conventional-commit type. The format is set up in `.goreleaser.yml`:

- `feat:` → **Features**
- `fix:` → **Bug Fixes**
- `security:` → **Security**
- everything else → **Others**

Anything starting with `docs:`, `test:`, `chore:`, or merge commits
is excluded. Write commit messages accordingly if you want them to
land in a release note.
