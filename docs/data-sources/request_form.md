---
page_title: "immuta_request_form Data Source - terraform-provider-immuta"
subcategory: ""
description: |-
  Looks up an existing Request app (formerly Marketplace) request form by name.
---

# immuta_request_form (Data Source)

Looks up an existing Request app (formerly Marketplace) request form by name,
with the documented `GET /api/request-form` search, for the request forms of
[`immuta_data_product`](../resources/data_product.md).

Like `immuta_data_product`, it uses the Request app API: set the provider's
`request_app_url` and `request_app_token`; see
[Authentication](../index.md#request-app-data-products).

## Example usage

```terraform
# A Request app request form, by its exact, case-sensitive name.
data "immuta_request_form" "standard" {
  name = "Standard Request Form"
}

resource "immuta_data_product" "customer_360" {
  name                     = "Customer 360"
  domains                  = ["0b6a7c1e-5d1f-4d43-9a8e-1f2b3c4d5e6f"]
  subject_matter_expert_id = "99aeadb5-352e-4872-9983-e628d399c36e"
  data_sources             = ["42"]

  # The same form can be used for both.
  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.standard.id
}
```

## Schema

### Required

- `name` (String) Name of the request form, for example
  `Standard Request Form`. The match is exact and case-sensitive.

### Read-only

- `id` (String) ID of the request form.

## Behaviour notes

- **Exactly one form must match.** No form with the name, or several, is an
  error.
