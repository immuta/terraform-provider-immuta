# Contributing

Thank you for your interest in improving this provider. It is not part of
the Immuta product and is not supported by Immuta Support, so please raise
questions and problems here on GitHub, not with Immuta Support.

## Before you start

- **Bugs and feature requests:** check the
  [existing issues](https://github.com/immuta/terraform-provider-immuta/issues)
  first, then open a new issue if yours is not there. See
  [Reporting issues](README.md#reporting-issues) for what to include.
- **Security vulnerabilities:** do not open a public issue. Use
  [private vulnerability reporting](https://github.com/immuta/terraform-provider-immuta/security/advisories/new).
- **Significant changes:** open an issue to discuss the change before you
  write code, especially for a new resource or a change to an existing schema.

## Workflow

1. Fork the repository and create a branch from `main`.
2. Make your change, together with tests, docs (`docs/`) and examples
   (`examples/`).
3. Run `make check`, and `make validate-examples` if you changed examples.
4. Open a pull request that explains what changed and why, and how you
   tested it.

CI runs lint, the unit tests, the build and example validation on every pull
request. It has no Immuta credentials, so it does not run the acceptance tests.

## Development setup

You need [Go](https://go.dev/dl/) 1.25 or later and
[Terraform](https://developer.hashicorp.com/terraform/downloads). The smoke
tests also need `curl` and `jq`.

```sh
make build      # build ./terraform-provider-immuta
make install    # install the binary into GOBIN
make fmt        # format the Go and Terraform sources
make check      # lint and unit tests
make help       # list every target
```

### Running a local build

This is also how to use the provider on platforms that have no release
binary. Install the build and point Terraform at it with a development
override. The command below replaces any existing `~/.terraformrc`, so merge
the block by hand if you already have one:

```sh
make install    # installs into $(go env GOPATH)/bin

cat > ~/.terraformrc <<TFRC
provider_installation {
  dev_overrides {
    "immuta/immuta" = "$(go env GOPATH)/bin"
  }

  direct {}
}
TFRC
```

With `dev_overrides` in place, skip `terraform init` and run `terraform plan`
directly. Remove the override when you have finished, so that Terraform goes
back to using the released provider.

### Repository layout

```
main.go                      provider entry point
internal/immutaapi/          typed client for the Immuta APIs
internal/provider/           provider, resources, data sources and their tests
internal/provider/testutil/  fake API server, parity helpers, fixtures
docs/                        registry documentation
examples/                    example configuration
scripts/                     smoke tests and example validation
```

## Testing

### Unit tests

```sh
make test
```

Unit tests need no Immuta tenant, credentials or network access. They fake
the API with `httptest` servers, and an in-memory implementation in
`internal/provider/testutil` drives the resources through real Terraform plans
and applies.

When `terraform` is not on your `PATH`, the tests that use the Terraform CLI
skip themselves rather than download a binary. CI installs Terraform, so they
always run there.

### Acceptance tests

Acceptance tests create and destroy real objects. **Run them against a sandbox
tenant, never production.**

```sh
export IMMUTA_HOST="https://sandbox.immuta.com"
export IMMUTA_API_KEY="..."
export IMMUTA_TEST_DOMAIN_NAME_PREFIX="tf-acc-$(whoami)"

make testacc
```

`TestAccDataProductResource` also needs the Request app API and an existing
domain, data source and subject matter expert. Without them it is skipped:

```sh
export IMMUTA_REQUEST_APP_URL="https://na.api.immutacloud.com/marketplace"
export IMMUTA_REQUEST_APP_TOKEN="..."
export IMMUTA_TEST_DATA_PRODUCT_DOMAIN_ID="..."
export IMMUTA_TEST_DATA_PRODUCT_SOURCE_ID="..."
export IMMUTA_TEST_DATA_PRODUCT_EXPERT_ID="..."
export IMMUTA_TEST_DATA_PRODUCT_REQUEST_FORM="Standard Request Form"
```

`TestAccGlobalUserDataSource` only reads. It needs the same Request app
variables and the username of an existing user:

```sh
export IMMUTA_TEST_GLOBAL_USER_USERNAME="jane.doe@example.com"
```

`TestAccDomainResourceForceDestroy` assigns data sources to a throwaway domain
by tag, then destroys the domain with `force_destroy`. It needs a tag on at
least one data source that is in no domain. Without it, it is skipped:

```sh
export IMMUTA_TEST_FORCE_DESTROY_TAG="..."
```

Every step checks Terraform state against a direct API read. A resource that
applies cleanly but stores the wrong state still fails.

### Smoke tests

These check connectivity, credentials and permissions without Terraform:

```sh
make smoke-domain   # create, read, update and delete a throwaway domain
make smoke-bim      # the same for a user, group, membership and attributes
```

Both print PASS or FAIL for each check and clean up after themselves, even
when a check fails. `smoke-bim` also checks the user delete endpoint that the
provider shares with the Immuta UI.

### Example validation

```sh
make validate-examples
```

This builds the provider and runs `terraform validate` against every example.
It needs no tenant or credentials.

## Conventions

- **Use documented APIs.** Use only documented Immuta API endpoints and
  attributes. The one exception is user deletion (see
  [`immuta_user`](docs/resources/user.md)). Do not add another undocumented
  endpoint without discussing it in an issue first.
- **Keep credentials safe.** Never log credentials or put them in URLs, and
  redact them from errors.
- **Retry with care.** Never retry creates. Retry only idempotent requests,
  and only after transient failures.
- **Surface drift.** Report remote drift instead of ignoring it.
- **Keep state compatible.** Do not change attribute names, types, IDs or
  Required, Optional and Computed semantics without a migration path.
- **Test behaviour changes.** Every behaviour change needs a test.

## Licence

By contributing, you agree that your contributions are licensed under the
[Apache 2.0 licence](LICENSE), which covers this repository only. This
repository is not part of the Immuta product.
