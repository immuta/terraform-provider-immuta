PKG_NAME=immuta
FULL_PKG_NAME=github.com/immuta/terraform-provider-immuta
VERSION_PLACEHOLDER=version.ProviderVersion
VERSION=$(shell git rev-parse --short=7 HEAD)
PROVIDER_VERSION=0.1.0
IMMUTA_PROVIDER_PATH=registry.terraform.io/immuta/immuta/$(PROVIDER_VERSION)/darwin_amd64

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
	mkdir -p ~/.terraform.d/plugins/$(IMMUTA_PROVIDER_PATH)/
	go build -o ~/.terraform.d/plugins/$(IMMUTA_PROVIDER_PATH)/terraform-provider-immuta -ldflags="-X $(FULL_PKG_NAME)/$(VERSION_PLACEHOLDER)=$(VERSION)"

test: fmtcheck
	go test -i $(TEST) || exit 1
	echo $(TEST) | \
		xargs -t -n4 go test $(TESTARGS) -timeout=30s -parallel=4

testacc: fmtcheck
	TF_ACC=1 go test $(TEST) -v ./... -parallel 20 $(TESTARGS) -timeout 120m

uninstall:
	@rm -vf ~/.terraform.d/plugins/$(IMMUTA_PROVIDER_PATH)/terraform-provider-immuta
