PKG_NAME=immuta
FULL_PKG_NAME=github.com/instacart/terraform-provider-immuta
VERSION_PLACEHOLDER=version.ProviderVersion
VERSION=$(shell git rev-parse --short=7 HEAD)
IMMUTA_PROVIDER_PATH=registry.terraform.io/instacart/immuta/$(PROVIDER_VERSION)/darwin_amd64

default: build

build: fmtcheck
	go install

fmt: format-tf
	gofmt -w $(GOFMT_FILES)

fmtcheck:
	@sh -c "'$(CURDIR)/scripts/gofmtcheck.sh'"

install: fmtcheck
	mkdir -p ~/Library/Application\ Support/io.terraform/plugins/$(IMMUTA_PROVIDER_PATH)/
	go build -o ~/Library/Application\ Support/io.terraform/plugins/$(IMMUTA_PROVIDER_PATH)/terraform-provider-immuta -ldflags="-X $(FULL_PKG_NAME)/$(VERSION_PLACEHOLDER)=$(VERSION)"

testacc: fmtcheck
	TF_ACC=1 go test $(TEST) -v ./... -parallel 20 $(TESTARGS) -timeout 120m
