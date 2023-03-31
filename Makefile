PKG_NAME=immuta
FULL_PKG_NAME=github.com/instacart/terraform-provider-immuta
VERSION_PLACEHOLDER=version.ProviderVersion
VERSION=$(shell git rev-parse --short=7 HEAD)
PROVIDER_VERSION=99.0.0
IMMUTA_PROVIDER_PATH=registry.terraform.io/instacart/immuta/$(PROVIDER_VERSION)/darwin_amd64

default: build

# bin generates the releasable binaries
bin: fmtcheck generate
	sh -c "'$(CURDIR)/scripts/build.sh'"

build: fmtcheck
	go install

fmt: format-tf
	gofmt -w $(GOFMT_FILES)

fmtcheck:
	@sh -c "'$(CURDIR)/scripts/gofmtcheck.sh'"

# generate runs `go generate` to build the dynamically generated
# source files.
generate:
	GOFLAGS=-mod=vendor go generate ./...

install: fmtcheck
	mkdir -p ~/Library/Application\ Support/io.terraform/plugins/$(IMMUTA_PROVIDER_PATH)/
	go build -o ~/Library/Application\ Support/io.terraform/plugins/$(IMMUTA_PROVIDER_PATH)/terraform-provider-immuta -ldflags="-X $(FULL_PKG_NAME)/$(VERSION_PLACEHOLDER)=$(VERSION)"

uninstall:
	@rm -vf $(DIR)/terraform-provider-immuta

.PHONY: install-immuta-provider
install-immuta-provider:
	aws s3 cp s3://infra-releases/terraform-provider-immuta/latest/darwin_amd64.tar.gz /tmp
	mkdir -p ~/Library/Application\ Support/io.terraform/plugins/$(IMMUTA_PROVIDER_PATH)/
	tar -xf /tmp/darwin_amd64.tar.gz -C ~/Library/Application\ Support/io.terraform/plugins/$(IMMUTA_PROVIDER_PATH)/

testacc: fmtcheck
	TF_ACC=1 go test $(TEST) -v ./... -parallel 20 $(TESTARGS) -timeout 120m
