# terraform-provider-immuta
This provider library wraps the stateless <https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api>.

## Quickstart
1. Download deps to `vendor/`: `go get .`
1. `make test`
1. `make`
1. Build a release: `make bin`

## Usage
1. Update the `~/.terraformrc` to be able to use local plugins:
``provider_installation {
    filesystem_mirror {
        path = "/Users/me/.terraform.d/plugins"
    }
}``
1. `make install`
1. Set `api_token` in `example.tf` or environment variable `IMMUTA_API_TOKEN`
1. Set `host` in `example.tf` or environment variable `IMMUTA_HOST`
1. `terraform init`
