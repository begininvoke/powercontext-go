# Workflows

All third-party actions are pinned to immutable commit SHAs.

- `ci.yml` runs generated-code drift checks, formatting, vet, standard and
  full-tag tests, the race detector, frozen-Oracle compatibility checks, and
  all three host-adapter suites.
- `provider-smoke.yml` is a manually dispatched, credentialed smoke test. It
  makes at most one generation request and one embedding request and never
  prints model input, model output, or credentials.
- `release.yml` builds the four supported OS/architecture pairs, packages the
  standard and full variants, emits checksums/SBOM/license inventories, and
  publishes matching multi-architecture OCI images.

Repository secrets are optional in normal CI. Only the manually dispatched
provider smoke consumes provider credentials.
