---
page_title: "immuta_global_user Data Source - terraform-provider-immuta"
subcategory: ""
description: |-
  Looks up the Immuta global user ID of an existing user by username.
---

# immuta_global_user (Data Source)

Looks up the Immuta global user ID of an existing user by username, with the
documented Request app (formerly Marketplace)
[`GET /api/metadata/users`](https://documentation.immuta.com/saas/developer-guides/api-intro/marketplace-api/metadata)
search. The global user ID is a UUID identifying a user across identity
managers. Use it where Immuta asks for one, such as
[`immuta_data_product`](../resources/data_product.md)`.subject_matter_expert_id`.

The Request app is the only documented API returning the global user ID, so
this data source needs the provider's `request_app_url` and
`request_app_token`; see
[Authentication](../index.md#request-app-data-products). For the numeric
profile ID, use [`immuta_user`](user.md) instead.

## Example usage

```terraform
# Look up a user's Immuta global user ID by username. Needs the provider's
# request_app_url and request_app_token.
data "immuta_global_user" "jane" {
  username = "jane.doe@immuta.com"
}

# iam_id is needed when the same username exists in several identity
# managers.
data "immuta_global_user" "sam" {
  username = "sam.smith"
  iam_id   = "bim"
}

resource "immuta_data_product" "customer_360" {
  name                     = "Customer 360"
  domains                  = ["0b6c1d1e-6a1f-4c0e-9d0c-1f2e3a4b5c6d"]
  subject_matter_expert_id = data.immuta_global_user.jane.global_user_id
  data_sources             = ["12"]
}
```

## Schema

### Required

- `username` (String) Username (login name) of the user to look up, the same
  value as `immuta_user.user_id`. The match is exact, including case.

### Optional

- `iam_id` (String) Identity manager to look in, for example `bim`. Needed
  when the same username exists in several identity managers.

### Read-Only

- `global_user_id` (String) Immuta global user ID of the user, a UUID.
- `id` (String) The global user ID, the same value as `global_user_id`.
- `name` (String) Display name of the user.
- `email` (String) Email address of the user, or an empty string when Immuta
  holds none.

`iam_id` is also set, as Immuta holds it.

## Behaviour notes

- **Exactly one user must match.** Immuta's search matches partially; the
  data source keeps exact username matches only. When none match, or several
  do (the same username in two identity managers), the error lists what was
  found and how to choose.
- **Search limit:** the search returns at most 100 users and has no paging.
  If a username is a prefix of many others and the user is not among the
  first 100 partial matches, the error says so.
- **Credentials:** a `401` means the Request app did not accept
  `request_app_token`, or `request_app_url` is not the tenant's region.
