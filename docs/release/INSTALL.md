# Install a PowerContext binary release

Every archive is self-describing and contains the CLI/Server binary, the
authoritative OpenAPI document, host adapters, Vec1, dependency licenses,
build metadata, an SPDX JSON SBOM, and an internal `SHA256SUMS` file.

Verify the downloaded archive and its detached SBOM against the release-level
`SHA256SUMS`, extract it, then verify the files inside the archive:

```sh
sha256sum --check SHA256SUMS
tar -xzf powercontext-*.tar.gz
cd powercontext-*/
sha256sum --check SHA256SUMS
./bin/powercontext --version
```

On macOS, use `shasum -a 256 -c SHA256SUMS` in place of `sha256sum`.

Vec1 remains opt-in, matching the server configuration contract. Point
`POWERCONTEXT_DATABASE_VEC1_EXTENSION` at the library under
`lib/powercontext/`. The Full archive also contains ONNX Runtime under
`lib/onnxruntime/`; set `POWERCONTEXT_ONNXRUNTIME_LIBRARY_DIR` to that
directory before selecting a `sentence-transformers:*` embedding model.

The binary does not require a Python runtime. Codex and Bub retain their thin
Python host adapters, and DSH retains its TypeScript adapter; all of them call
the Go Server over HTTP.
