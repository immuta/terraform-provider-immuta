BINARY  := terraform-provider-immuta
VERSION ?= dev
LDFLAGS := -X main.version=$(VERSION)

.DEFAULT_GOAL := check

## check: run every check that does not need an Immuta tenant
.PHONY: check
check: lint test

## build: build the provider binary into the working directory
.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## install: install the provider binary into GOBIN for dev overrides
.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" .

## bin: cross-compile release tarballs into releases/ (used by CI)
.PHONY: bin
bin:
	./scripts/build.sh

## fmt: format Go and Terraform sources
.PHONY: fmt
fmt:
	gofmt -s -w .
	@if command -v terraform >/dev/null 2>&1; then \
		terraform fmt -recursive examples; \
	else \
		echo "terraform is not installed, skipping terraform fmt"; \
	fi

## fmt-check: fail when sources are not formatted
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -s -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "these files are not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi
	@if command -v terraform >/dev/null 2>&1; then \
		terraform fmt -check -recursive examples; \
	else \
		echo "terraform is not installed, skipping terraform fmt -check"; \
	fi

## vet: run go vet
.PHONY: vet
vet:
	go vet ./...

## lint: formatting, vet and golangci-lint when it is installed
.PHONY: lint
lint: fmt-check vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint is not installed, skipping"; \
	fi

## test: unit tests, no Immuta tenant and no credentials required
.PHONY: test
test:
	go test ./... -timeout 10m

## testacc: acceptance tests against a real sandbox tenant
## Requires IMMUTA_HOST, IMMUTA_API_KEY and IMMUTA_TEST_DOMAIN_NAME_PREFIX.
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./internal/provider/ -run '^TestAcc' -v -timeout 30m

## smoke-domain: exercise the documented domain endpoints with curl only
.PHONY: smoke-domain
smoke-domain:
	./scripts/smoke_domain.sh

## smoke-bim: exercise the BIM identity endpoints with curl only
.PHONY: smoke-bim
smoke-bim:
	./scripts/smoke_bim.sh

## validate-examples: terraform validate the example configurations
.PHONY: validate-examples
validate-examples:
	./scripts/validate_examples.sh

## clean: remove build output
.PHONY: clean
clean:
	rm -f $(BINARY)

## help: list the available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

.PHONY: release
release:
	@printf "Enter version (e.g. 0.1.0-alpha.1): " && read VERSION; \
	if [ -z "$$VERSION" ]; then \
		echo "Error: version is required"; \
		exit 1; \
	fi; \
	TAG="v$$VERSION"; \
	\
	echo "==> Releasing $(BINARY) $$TAG"; \
	\
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: working tree is not clean. Commit your changes first."; \
		exit 1; \
	fi; \
	\
	if git rev-parse "$$TAG" >/dev/null 2>&1; then \
		echo "Error: tag $$TAG already exists"; \
		exit 1; \
	fi; \
	\
	echo "==> Building provider"; \
	go build -o ./$(BINARY) . || exit 1; \
	\
	echo "==> Preparing release"; \
	rm -rf ./release; \
	mkdir -p ./release; \
	cp ./$(BINARY) ./release/$(BINARY)_v$$VERSION; \
	\
	echo "==> Creating archive"; \
	cd ./release && \
	zip "$(BINARY)_$$VERSION_darwin_arm64.zip" \
		"$(BINARY)_v$$VERSION" || exit 1; \
	cd ..; \
	\
	echo "==> Creating Git tag $$TAG"; \
	git tag "$$TAG" || exit 1; \
	\
	echo "==> Pushing Git tag"; \
	git push origin "$$TAG" || exit 1; \
	\
	echo "==> Creating GitHub release"; \
	gh release create "$$TAG" \
		"./release/$(BINARY)_$$VERSION_darwin_arm64.zip" \
		--prerelease \
		--generate-notes || exit 1; \
	\
	echo ""; \
	echo "Release $$TAG created successfully!"
