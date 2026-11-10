---
page_title: "immuta_user_attribute Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Assigns one attribute value to an Immuta user with the documented authorizations API.
---

# immuta_user_attribute (Resource)

Assigns one attribute value to an [Immuta user](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/bim)
with `PUT /bim/iam/{iamid}/user/{userid}/authorizations/{name}/{value}`.

Attributes drive attribute-based access control. Policies that match on an
attribute start applying to the user as soon as it is assigned.

Managing attributes requires the `USER_ADMIN` permission.

## Example usage

```terraform
resource "immuta_user_attribute" "jane_department" {
  user_id         = immuta_user.jane.user_id
  user_profile_id = immuta_user.jane.id
  name            = "Department"
  value           = "Finance"
}
```

One resource holds one value. Assign several resources to give a user several
values of the same attribute:

```terraform
resource "immuta_user_attribute" "jane_regions" {
  for_each = toset(["APAC", "EMEA"])

  user_id         = immuta_user.jane.user_id
  user_profile_id = immuta_user.jane.id
  name            = "Region"
  value           = each.value
}
```

For a user Terraform does not manage, such as one synchronised from an
external identity manager, look the user up with the
[`immuta_user`](../data-sources/user.md) data source:

```terraform
data "immuta_user" "ada" {
  email = "ada.lovelace@immuta.com"
}

resource "immuta_user_attribute" "ada_department" {
  iam_id          = data.immuta_user.ada.iam_id
  user_id         = data.immuta_user.ada.user_id
  user_profile_id = data.immuta_user.ada.id
  name            = "Department"
  value           = "Engineering"
}
```

## Schema

### Required

- `user_id` (String) Username of the user, for example
  `immuta_user.jane.user_id`. Changing it replaces the assignment.
- `user_profile_id` (Number) Numeric profile ID of the same user, for example
  `immuta_user.jane.id`. Changing it replaces the assignment.
- `name` (String) Name of the attribute, for example `Department`. Changing it
  replaces the assignment.
- `value` (String) Value of the attribute, for example `Finance`. Changing it
  replaces the assignment.

### Optional

- `iam_id` (String) Identity manager the user belongs to. Defaults to `bim`.
  Changing it replaces the assignment.

### Read-Only

- `id` (String) Identifier of the assignment, in the form
  `{iam_id}/{user_profile_id}/{user_id}/{name}/{value}`.

## Why two identifiers are required

Immuta identifies a user by **username** on the documented attribute write
endpoints and by **numeric profile ID** on the documented user read endpoint.
Neither identifier works on both, and there is no documented way to translate
one into the other, so the resource takes both. Take them from the same
`immuta_user` resource and they stay in step.

## Behaviour notes

- **Nothing is updated in place.** Every argument replaces the assignment,
  because assigning and removing an attribute are the only documented
  operations. Replacement removes the old value before adding the new one.
- **Only attributes on the user's own profile are managed.** The read checks
  `bimAuthorizations`, which holds attributes assigned directly to the user,
  rather than `authorizations`, which also contains everything inherited from
  their groups. An attribute that only arrives through a group is not treated
  as owned by this resource.
- **Remote changes show up as drift.** An attribute removed in the Immuta UI
  is removed from state and planned for reassignment.
- **Attributes assigned outside Terraform are left alone.** The resource only
  ever adds and removes the one name and value pair it manages.

## Destructive behaviour

Destroying an `immuta_user_attribute` removes that one attribute value from
the user. The user is not deleted.

Removing an attribute revokes access granted by every policy that matches on
it, which can be a large blast radius for a single small change. Check which
policies reference the attribute before removing it.

## Testing this resource

```sh
export IMMUTA_HOST="https://sandbox.immuta.com"
export IMMUTA_API_KEY="..."

make smoke-bim
make testacc
```

## Import

Assignments are imported with
`{iam_id}/{user_profile_id}/{user_id}/{name}/{value}`.

```shell
terraform import immuta_user_attribute.jane_department 'bim/18/jane.doe@immuta.com/Department/Finance'
```

Attribute names and values that contain a `/` cannot be imported, because the
import ID uses `/` as its separator.
