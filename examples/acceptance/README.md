# Manual verification example

This configuration creates a single domain in a sandbox Immuta tenant. Use it
to verify the provider by hand, in addition to `make testacc`.

## Prerequisites

```sh
export IMMUTA_HOST="https://example.immuta.com"
export IMMUTA_API_KEY="..."          # never commit this value
export TF_VAR_domain_name="tf-manual-$(whoami)"
```

Build and install the provider, then point Terraform at the local build:

```sh
make install

cat > ~/.terraformrc <<'TFRC'
provider_installation {
  dev_overrides {
    "immuta/immuta" = "REPLACE_WITH_GOBIN"
  }
  direct {}
}
TFRC
```

`REPLACE_WITH_GOBIN` is the output of `go env GOPATH`/bin.

## Run

With `dev_overrides` in place, skip `terraform init`:

```sh
terraform plan
terraform apply
terraform destroy
```

Always run this against a sandbox tenant, never production, and destroy the
domain when you are finished.
