---
page_title: "immuta_data_product Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages an Immuta data product with the Request app (formerly Marketplace) API.
---

# immuta_data_product (Resource)

Manages an Immuta [data product](https://documentation.immuta.com/saas/developer-guides/api-intro/marketplace-api/data-product)
with the Request app (formerly Marketplace) API.

Domains are referenced by ID, usually from `immuta_domain`, and data sources
by ID, usually from the
[`immuta_domain_data_sources`](../data-sources/domain_data_sources.md) data
source.

~> **Separate credential.** The Request app API is served from a regional URL
(`https://{na|eu|ap}.api.immutacloud.com/marketplace`) and authenticates with
an Immuta personal access token, not the provider's `api_key`. Set the
provider's `request_app_url` and `request_app_token`, or the
`IMMUTA_REQUEST_APP_URL` and `IMMUTA_REQUEST_APP_TOKEN` environment
variables; see [Authentication](../index.md#request-app-data-products).
The domain data sources lookup below still uses the Govern API and `api_key`.

```terraform
provider "immuta" {
  host            = "https://example.immuta.com"
  request_app_url = "https://na.api.immutacloud.com/marketplace"
  # api_key from IMMUTA_API_KEY, request_app_token from IMMUTA_REQUEST_APP_TOKEN
}
```

The Immuta user who owns the personal access token needs permission to manage
data products in the domains used.

## Example usage

```terraform
resource "immuta_domain" "sales" {
  name = "Sales"
}

# Data sources are registered and assigned to the domain outside this
# configuration; the data source lists them.
data "immuta_domain_data_sources" "sales" {
  domain_id = immuta_domain.sales.id
}

# The subject matter expert, by username.
data "immuta_global_user" "expert" {
  username = "jane.doe@immuta.com"
}

# Immuta requires request forms to create a data product.
data "immuta_request_form" "standard" {
  name = "Standard Request Form"
}

resource "immuta_data_product" "customer_360" {
  name        = "Customer 360"
  description = "Everything about customers"
  domains     = [immuta_domain.sales.id]

  # The subject matter expert's Immuta global user ID.
  subject_matter_expert_id = data.immuta_global_user.expert.global_user_id

  data_sources = data.immuta_domain_data_sources.sales.ids

  # The same form can be used for both.
  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.standard.id
}

# Choosing the id keeps the data product the same in every environment.
resource "immuta_data_product" "orders" {
  id                       = "4c8f6d82-e4c6-4478-92eb-d9342500f006"
  name                     = "Orders"
  domains                  = [immuta_domain.sales.id]
  subject_matter_expert_id = "99aeadb5-352e-4872-9983-e628d399c36e"
  data_sources = [
    for data_source in data.immuta_domain_data_sources.sales.data_sources :
    data_source.id if startswith(data_source.name, "Orders")
  ]

  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.standard.id
}
```

## Schema

### Required

- `name` (String) Name of the data product. Immuta requires it to be unique.
- `domains` (Set of String) IDs of the domains the data product belongs to,
  usually `immuta_domain.<name>.id`. At least one is required.
- `subject_matter_expert_id` (String) Immuta global user ID (a UUID) of the
  data product's subject matter expert, for example from
  [`immuta_global_user`](../data-sources/global_user.md). This is not the
  numeric profile ID that `immuta_user.id` holds.
- `data_sources` (Set of String) IDs of the Immuta data sources in the data
  product, for example from `data.immuta_domain_data_sources.<name>.ids`.

### Optional

- `id` (String) ID of the data product, a UUID. Set it to keep the same ID in
  every environment; otherwise Immuta assigns one. Changing it replaces the
  data product.
- `description` (String) Description of the data product.
- `suspended` (Boolean) Whether access to the data product's data sources is
  suspended. Defaults to `false`.
- `data_access_request_form_id` (String) ID of the request form users fill in
  to request access to the data product, for example from
  [`immuta_request_form`](../data-sources/request_form.md).
- `masking_exception_request_form_id` (String) ID of the request form users
  fill in to request a masking exception. It can be the same form as
  `data_access_request_form_id`.

The two request form IDs are set together. Immuta requires them to create a
data product, so a new data product without them fails at plan time. They
may be left out for an existing, for example imported, data product.

## Behaviour notes

- **Order is not a change.** `domains` and `data_sources` are sets.
- **Data sources are kept in step.** Adding or removing one changes only the
  data product's data sources, with `PUT /api/data-product/{id}/datasources`.
- **The rest is updated in place** with `PUT /api/data-product/{id}`, which
  receives every managed attribute. Removing `description` clears it.
- **`suspended`** is not part of the create payload, so a data product
  configured as suspended is suspended straight after it is created.
- **Drift is detected** for every attribute except the request forms: a change
  made in Immuta shows as a diff and is put back on the next apply. A data
  product deleted in Immuta is removed from state and created again.
- **Request forms are write-only.** Immuta does not return them, so state
  keeps the configured IDs, a form changed in Immuta is not detected, and an
  import leaves them empty until the next apply sends them.
- **Not managed:** review flows, the deprecated `requestFormId`, and the
  deprecated `dataAccessPolicy` and `maskingExceptionPolicy`.

## Import

Data products are imported with their ID.

```shell
# Data products are imported with their id.
terraform import immuta_data_product.orders 4c8f6d82-e4c6-4478-92eb-d9342500f006
```
