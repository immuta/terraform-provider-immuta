---
page_title: "immuta_group_user Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Adds one user to one Immuta group with the documented /bim/group/{groupId}/user API.
---

# immuta_group_user (Resource)

Adds one user to one [Immuta group](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/bim).

Each membership is its own resource. Memberships created in the Immuta UI or
by another Terraform configuration are left alone, so this resource can be
adopted gradually on an existing tenant.

Managing memberships requires the `USER_ADMIN` permission.

## Example usage

```terraform
resource "immuta_group_user" "jane_engineering" {
  group_id = immuta_group.engineering.id
  user_id  = immuta_user.jane.user_id
}
```

For a user Terraform does not manage, such as one synchronised from an
external identity manager, look the user up with the
[`immuta_user`](../data-sources/user.md) data source:

```terraform
data "immuta_user" "grace" {
  email = "grace.hopper@immuta.com"
}

resource "immuta_group_user" "grace_engineering" {
  group_id = immuta_group.engineering.id
  iam_id   = data.immuta_user.grace.iam_id
  user_id  = data.immuta_user.grace.user_id
}
```

## Schema

### Required

- `group_id` (Number) Numeric ID of the group. Changing it replaces the
  membership.
- `user_id` (String) Username of the user. Changing it replaces the
  membership.

### Optional

- `iam_id` (String) Identity manager the user belongs to. Defaults to `bim`.
  Changing it replaces the membership.

### Read-Only

- `id` (String) Identifier of the membership, in the form
  `{iam_id}/{user_id}/{group_id}`.
- `membership_id` (Number) Numeric ID Immuta assigns to the membership itself.
  It is read back from
  `GET /bim/iam/{iamid}/user/{userid}/groups`, which is the only documented
  way to recover it, and is what removing the user from the group uses.

## Behaviour notes

- **Nothing is updated in place.** Every argument replaces the membership,
  because adding and removing a user from a group are the only documented
  operations.
- **Remote changes show up as drift.** A membership removed in the Immuta UI
  is removed from state and planned for recreation.
- **Adding a user twice fails.** Immuta rejects a duplicate membership with a
  conflict, which the provider surfaces rather than hiding. If the user is
  already a member, import the membership instead of creating it.

## Destructive behaviour

Destroying an `immuta_group_user` removes the user from the group. Neither the
user nor the group is deleted.

The user immediately loses the attributes the group carries, so any policy
that grants them access through those attributes stops applying. Check what
the group grants before removing members from it.

## Testing this resource

```sh
export IMMUTA_HOST="https://sandbox.immuta.com"
export IMMUTA_API_KEY="..."

make smoke-bim
make testacc
```

## Import

Memberships are imported with `{iam_id}/{user_id}/{group_id}`.

```shell
terraform import immuta_group_user.jane_engineering 'bim/jane.doe@immuta.com/12'
```

The provider looks the membership ID up from the user's group list during the
first read, so it does not need to be supplied.
