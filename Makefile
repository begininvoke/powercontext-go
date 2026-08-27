GO ?= go
GOFMT ?= gofmt
GOCACHE ?=
GOMODCACHE ?=

STANDARD_TAGS := sqlite_fts5
FULL_TAGS := sqlite_fts5,local_embeddings,ORT
VERSION ?= devel
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)

.PHONY: generate check-generated fmt fmt-check vet test test-sqlite test-race test-full test-oceanbase-live \
	build build-full smoke smoke-full check package-standard package-full clean

generate:
	$(GO) generate ./openapi
	$(GO) run ./tools/mcp-schema-generate

check-generated:
	$(GO) generate ./openapi
	$(GO) run ./tools/mcp-schema-generate
	$(GO) run ./tools/traceability-generate -check
	git diff --exit-code -- openapi api/v1 client/invoker_gen.go internal/mcpapi/schemas_gen.go integrations/dsh/plugins/powercontext/src/operations.generated.ts

fmt:
	$(GOFMT) -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	@files="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-sqlite:
	CGO_ENABLED=1 $(GO) test -tags '$(STANDARD_TAGS)' ./...

test-race:
	CGO_ENABLED=1 $(GO) test -race ./...

test-full:
	@test -d "$(TOKENIZERS_LIB_DIR)" || { echo 'TOKENIZERS_LIB_DIR must contain libtokenizers.a' >&2; exit 2; }
	CGO_ENABLED=1 CGO_LDFLAGS="$(CGO_LDFLAGS) -L$(TOKENIZERS_LIB_DIR)" \
		$(GO) test -tags '$(FULL_TAGS)' ./...

test-oceanbase-live:
	@test -n "$$POWERCONTEXT_TEST_OCEANBASE_URL" || { echo 'POWERCONTEXT_TEST_OCEANBASE_URL must name a dedicated OceanBase MySQL-mode database' >&2; exit 2; }
	$(GO) test -count=1 -run TestLiveOceanBaseProfileSmoke -v ./test/e2e

build:
	mkdir -p bin
	CGO_ENABLED=1 $(GO) build -tags '$(STANDARD_TAGS)' -trimpath \
		-ldflags '$(LDFLAGS)' -o bin/powercontext ./cmd/powercontext

build-full:
	@test -d "$(TOKENIZERS_LIB_DIR)" || { echo 'TOKENIZERS_LIB_DIR must contain libtokenizers.a' >&2; exit 2; }
	mkdir -p bin
	CGO_ENABLED=1 CGO_LDFLAGS="$(CGO_LDFLAGS) -L$(TOKENIZERS_LIB_DIR)" \
		$(GO) build -tags '$(FULL_TAGS)' -trimpath -ldflags '$(LDFLAGS)' \
		-o bin/powercontext-full ./cmd/powercontext

smoke: build
	$(GO) run ./tools/process-smoke -binary bin/powercontext -version "$(VERSION)"

smoke-full: build-full
	$(GO) run ./tools/process-smoke -binary bin/powercontext-full -version "$(VERSION)"

package-standard: build
	$(GO) run ./tools/release package \
		-binary bin/powercontext -edition standard \
		-version "$(VERSION)" -commit "$(COMMIT)" -build-date "$(BUILD_DATE)" \
		-output dist -syft "$(SYFT)"

package-full: build-full
	@test -d "$(ONNXRUNTIME_LIB_DIR)" || { echo 'ONNXRUNTIME_LIB_DIR must contain ONNX Runtime libraries' >&2; exit 2; }
	$(GO) run ./tools/release package \
		-binary bin/powercontext-full \
		-onnxruntime-dir "$(ONNXRUNTIME_LIB_DIR)" -edition full \
		-version "$(VERSION)" -commit "$(COMMIT)" -build-date "$(BUILD_DATE)" \
		-output dist -syft "$(SYFT)"

check: check-generated fmt-check vet test test-sqlite

clean:
	$(RM) -r bin dist coverage
