# Install a PowerContext binary release

Every archive is self-describing and contains the CLI/Server binary, the
authoritative OpenAPI document, host adapters, embedded sqlite-vec, dependency licenses,
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

sqlite-vec 0.1.9 is statically embedded in every binary; no extension path or
host SQLite package is required. The Full archive also contains ONNX Runtime under
`lib/onnxruntime/`; set `POWERCONTEXT_ONNXRUNTIME_LIBRARY_DIR` to that
directory before selecting a `sentence-transformers:*` embedding model.

The binary does not require a Python runtime. Codex and Bub retain their thin
Python host adapters, and DSH retains its TypeScript adapter; all of them call
the Go Server over HTTP.
