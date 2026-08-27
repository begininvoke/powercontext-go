# GitHub CI/CD

All third-party actions are pinned to immutable commit SHAs. Go setup and
native release-asset acquisition live in composite actions under
`.github/actions`, so CI, candidate builds, and releases use the same locked
module and checksum-verification path.

## Continuous integration

- `ci.yml` checks generated contracts, formatting, vet, module tidiness, the
  race detector, high-risk fuzz targets, the frozen Python Oracle, live
  OceanBase behavior, all four native release platforms, standard and Full
  build tags, host adapters, and the evaluation harness.
- `provider-smoke.yml` is a manually dispatched, credentialed test that makes
  at most one generation request and one embedding request. It does not print
  model input, model output, or credentials.
- `license-check.yml` runs SkyWalking Eyes 0.8.0 against the explicit source
  surface in `.licenserc.yaml`. Developers can repair missing headers with
  `make license-fix` and reproduce CI locally with `make license-check`.

## Candidate delivery

- `build-artifacts.yml` manually builds checksummed Linux amd64 standard, Full,
  or combined binary bundles. It emits the same SBOM/license metadata as a
  release and exercises the real CLI, HTTP, MCP, and SQLite restart surface
  before upload.
- `build-docker.yml` manually builds standard, Full, or combined multi-platform
  OCI archives for Linux amd64/arm64. It includes SBOM/provenance attestations,
  checksum files, and boots each selected amd64 image before upload.

Candidate workflows accept an optional source ref and semantic version. When
the version is omitted they generate a traceable development version from the
workflow run and source commit. Their artifacts expire after 30 days and are
never pushed to GHCR or attached to a GitHub Release.

## Release delivery

- `release.yml` runs when a GitHub Release is published. A single prepare job
  freezes version, commit, and build time for every downstream artifact. It
  builds standard and Full bundles for Linux/macOS on amd64/arm64, publishes
  multi-platform images to GHCR, attaches 8 archives, 8 detached SPDX SBOMs,
  an immutable image-digest manifest, and one release-level checksum manifest.
- `release-verify.yml` is both reusable and manually dispatchable. After
  publication it downloads the public release assets again, verifies the full
  inventory and nested checksums, exercises both Linux binaries, inspects both
  GHCR manifests by digest, and boots the published standard image.

The release tag may be `vX.Y.Z`, `powercontext-vX.Y.Z`, or `X.Y.Z`. Publishing
a GitHub prerelease intentionally uses the same pipeline.

Normal CI and candidate builds require no repository secrets. The provider
smoke uses secrets from the protected `provider-smoke` environment. Release
image publication and verification use the workflow-scoped `GITHUB_TOKEN` with
only the declared `contents` and `packages` permissions.

Unlike the Python repository, this repository has no generated documentation
site or package-index publication target, so CI/CD deliberately does not add a
GitHub Pages or PyPI-equivalent deployment job. Binary archives and GHCR images
are the Go product's authoritative delivery surfaces.
