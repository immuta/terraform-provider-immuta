---
page_title: "immuta_user Data Source - terraform-provider-immuta"
subcategory: ""
description: |-
  Looks up an existing Immuta user by email address or username.
---

# immuta_user (Data Source)

Looks up an existing Immuta user, in any identity manager, by email address or
username, with the documented
[`GET /bim/user`](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/bim)
search. Use it to reference users Terraform does not manage, such as users
synchronised from an external identity manager: connection data object
owners, [`immuta_user_attribute`](../resources/user_attribute.md) and
[`immuta_group_user`](../resources/group_user.md).

## Example usage

```terraform
# Look a user up by email address.
data "immuta_user" "jane" {
  email = "jane.doe@immuta.com"
}

# Or by username, in a given identity manager. iam_id is needed when the
# same person exists in several identity managers.
data "immuta_user" "sam" {
  user_id = "sam.smith@contractor.example"
  iam_id  = "bim"
}

# id is the numeric profile ID as a string, like immuta_user.id. Documents
# built with yamlencode() or jsonencode() need it as a number.
locals {
  data_object_defaults = yamlencode({
    owners = [{ type = "USER", id = tonumber(data.immuta_user.jane.id) }]
  })
}
```

## Schema

### Optional

Exactly one of `email` and `user_id` is required.

- `email` (String) Email address of the user to look up. The match is exact
  but ignores case.
- `user_id` (String) Username of the user to look up. The match is exact.
- `iam_id` (String) Identity manager to look in, for example `bim`. Needed
  when the same person exists in several identity managers.

### Read-Only

- `id` (String) Numeric profile ID of the user, the same identifier as
  `immuta_user.id`. Use `tonumber()` where a JSON or YAML document built with
  `jsonencode()` or `yamlencode()` needs it as a number; Immuta returns it as
  a number, so a string would show as a change on every plan.
- `name` (String) Display name of the user.
- `disabled` (Boolean) Whether the user is disabled. Immuta's search only
  returns enabled users, so this is `false`.

`email`, `user_id` and `iam_id` are also set, as Immuta holds them.

## Behaviour notes

- **Exactly one user must match.** Immuta's search matches partially; the
  data source keeps exact matches only. When none match, or several do (the
  same email in two identity managers), the error lists what was found and
  how to choose.
- **Disabled users are not found.**
- **Permissions:** searching users needs the `USER_ADMIN` permission, like
  `immuta_user`.
- **Not for data product subject matter experts:**
  `immuta_data_product.subject_matter_expert_id` takes a global user ID (a
  UUID), which this endpoint does not return. Use
  [`immuta_global_user`](global_user.md) for it.
