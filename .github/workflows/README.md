# GitHub CI/CD

The workflow topology follows the Python repository at
`3a6cb0151670eaff7dc0293466edd673124e80da`. A workflow or default-CI job belongs here only when it has a Python
counterpart or enforces a Go release constraint that does not exist in Python.

| Python workflow | Go workflow | Deliberate adaptation |
| --- | --- | --- |
| `master.yml` | `master.yml` | Go module, formatting, vet, generated transport contracts, Go tests, and the same Pi package replace Python lock, prek, and interpreter tests. |
| `e2e-harness.yml` | `e2e-harness.yml` | The same validate/SQLite/OceanBase/evidence lifecycle drives the Go process and live OceanBase acceptance tests. |
| `license-check.yml` | `license-check.yml` | Both call SkyWalking Eyes 0.8.0 directly. `make license-check` and `make license-fix` remain local entry points. |
| `deploy-docs.yml` | `deploy-docs.yml` | Both build locked Zensical documentation and deploy GitHub Pages. |
| `build-artifacts.yml` | `build-artifacts.yml` | Go binary bundles replace Python wheel and offline-wheel bundles; standard and Full editions are release requirements. |
| `build-docker.yml` | `build-docker.yml` | Go standard and Full images replace the Python server image. |
| `release.yml` | `release.yml` | GitHub binary assets and GHCR replace PyPI; release verification and documentation deployment keep the same gates. |
| `release-verify.yml` | `release-verify.yml` | Verification exercises published Go archives and image digests instead of Python distributions. |

`master.yml` intentionally does not clone or execute the Python repository. Frozen Python data remains committed under
`test/conformance/testdata` and is exercised by normal Go tests. Regenerating that data is a maintainer operation, not
a dependency of every pull request.

Credentialed provider smoke tests remain opt-in through `make real-provider-test`, matching Python's local
`real-e2e-test` policy. Four-platform standard/Full builds remain in candidate and release workflows, where they verify
the actual Go delivery surface without expanding default CI.
