---
page_title: "Immuta Provider"
subcategory: ""
description: |-
  Manage Immuta SaaS objects with the documented Immuta APIs.
---

# Immuta Provider

The Immuta provider manages Immuta SaaS objects through Immuta's published
APIs:

- the [V1 API](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api)
  for domains, identity management, tags, identifiers, frameworks and data
  sources
- the [V2 API](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v2-api/policies)
  for global policies
- the [Connections API](https://documentation.immuta.com/saas/developer-guides/api-intro/connections-api)
  for connections and data objects
- the [Request app API](https://documentation.immuta.com/saas/developer-guides/api-intro/marketplace-api)
  for data products

There is one exception. To delete a user, `immuta_user` uses the same request
as the Immuta UI, because the published API can only disable users. For the
details, see [`immuta_user`](resources/user.md).

~> This provider is not part of the Immuta product and is not supported by
Immuta Support. You use it at your own risk. Report
problems as
[GitHub issues](https://github.com/immuta/terraform-provider-immuta/issues).

## Example usage

```terraform
terraform {
  required_providers {
    immuta = {
      source  = "immuta/immuta"
      version = "0.1.0-alpha" # pre-release versions must be pinned exactly
    }
  }
}

# The API key is read from IMMUTA_API_KEY.
provider "immuta" {
  host = "https://example.immuta.com"
}

resource "immuta_domain" "research" {
  name        = "Research"
  description = "Domain for the research organisation"
}
```

## Authentication

The provider authenticates with an Immuta API key, which it sends in the
`Authorization` header of every request.

Supply the key with the `IMMUTA_API_KEY` environment variable so that the
credential is never written to configuration or to Terraform state:

```sh
export IMMUTA_HOST="https://example.immuta.com"
export IMMUTA_API_KEY="..."
```

The `api_key` argument is marked sensitive, so Terraform does not print it in
plan or apply output. The provider also redacts the key from any error message
that happens to contain it.

The Immuta user that owns the key needs the permissions documented for the
endpoints in use. Domain management requires the `GOVERNANCE` permission.

### Request app (data products)

`immuta_data_product` uses the
[Request app (formerly Marketplace) API](https://documentation.immuta.com/saas/developer-guides/api-intro/marketplace-api),
which Immuta serves from a regional URL rather than the tenant host, and which
authenticates with a personal access token (PAT) sent as a bearer token, not
with the API key. Generate the PAT on the
[Developer Settings page](https://documentation.immuta.com/saas/developer-guides/api-intro/authentication),
and use the URL of the region the tenant is in:

| Region | `request_app_url` |
| --- | --- |
| North America | `https://na.api.immutacloud.com/marketplace` |
| European Union | `https://eu.api.immutacloud.com/marketplace` |
| Asia Pacific | `https://ap.api.immutacloud.com/marketplace` |

```sh
export IMMUTA_REQUEST_APP_URL="https://na.api.immutacloud.com/marketplace"
export IMMUTA_REQUEST_APP_TOKEN="..."
```

Both are optional: without them every other resource works, and
`immuta_data_product` reports that the Request app is not configured. A PAT
used against the wrong region is rejected as not authenticated (`401`).

## Schema

### Optional

- `host` (String) Base URL of the Immuta tenant, for example
  `https://example.immuta.com`. A bare hostname is also accepted and defaults
  to `https`. May also be set with the `IMMUTA_HOST` environment variable.
- `api_key` (String, Sensitive) Immuta API key sent in the `Authorization`
  header. May also be set with the `IMMUTA_API_KEY` environment variable,
  which is the recommended way to supply it.
- `request_app_url` (String) Regional base URL of the Request app API, used
  by `immuta_data_product`, for example
  `https://na.api.immutacloud.com/marketplace`. May also be set with the
  `IMMUTA_REQUEST_APP_URL` environment variable. Required with
  `request_app_token`.
- `request_app_token` (String, Sensitive) Immuta personal access token for the
  Request app API, sent as a bearer token. Separate from `api_key`. May also be
  set with the `IMMUTA_REQUEST_APP_TOKEN` environment variable, which is the
  recommended way to supply it. Required with `request_app_url`.
- `request_timeout` (String) Timeout for a single API request, as a Go
  duration such as `30s`. Defaults to `30s`.
- `retry_count` (Number) Number of retries for idempotent requests that fail
  with a transient error such as HTTP 429, 502, 503 or 504. Must be between 0
  and 5. Defaults to 2. Creates are never retried, so a transient failure
  cannot produce duplicate objects.

`host` and `api_key` are both required at runtime: set each one either in the
provider block or through its environment variable. `request_app_url` and
`request_app_token` are needed only for `immuta_data_product`, and must be set
together.
