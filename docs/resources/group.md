---
page_title: "immuta_group Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages an Immuta group with the documented /bim/group API.
---

# immuta_group (Resource)

Manages an [Immuta group](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/bim).

Groups collect users so that attributes and policies can be applied to them
together. Members of a group inherit the group's attributes.

Managing groups requires the `USER_ADMIN` permission.

## Example usage

```terraform
resource "immuta_group" "engineering" {
  name        = "Engineering"
  description = "Engineering department"
  email       = "engineering@immuta.com"
}
```

## Schema

### Required

- `name` (String) Name of the group. Changing the name updates the existing
  group in place rather than replacing it.

### Optional

- `iam_id` (String) Identity manager the group belongs to. Defaults to `bim`,
  Immuta's built-in identity manager. Changing it replaces the group.
- `description` (String) Description of the group that is displayed to users.
  Removing the argument clears the description in Immuta.
- `email` (String) Email address associated with the group.

### Read-Only

- `id` (String) Numeric ID of the group, assigned by Immuta.
- `created_at` (String) Timestamp of when the group was created.
- `updated_at` (String) Timestamp of when the group was last updated.

Membership is managed with [`immuta_group_user`](group_user.md) and attributes
with [`immuta_group_attribute`](group_attribute.md), so that memberships and
attributes owned elsewhere are not overwritten.

## Behaviour notes

- **Name changes update in place.** The documented `PUT /bim/group/{groupId}`
  endpoint changes the name, description and email, so renaming a group does
  not destroy it or its memberships.
- **Remote changes show up as drift.** A description edited in the Immuta UI
  produces a diff on the next plan. A group deleted outside Terraform is
  removed from state and planned for recreation.
- **An unset description stays null.** Immuta returns no description for a
  group that has none; the provider keeps that as `null` instead of producing
  a perpetual diff.

## Destructive behaviour

Destroying an `immuta_group` deletes the group in Immuta. Members are not
deleted, but they lose the group's attributes, so any policy that grants
access through those attributes stops applying to them.

Work out which policies depend on the group before removing it, and try it in
a sandbox tenant first.

## Testing this resource

```sh
export IMMUTA_HOST="https://sandbox.immuta.com"
export IMMUTA_API_KEY="..."

make smoke-bim
make testacc
```

Both commands create throwaway groups and clean up after themselves. Never
point them at a production tenant.

## Import

Groups are imported with the numeric group ID assigned by Immuta.

```shell
terraform import immuta_group.engineering 12
```

The numeric ID is the `id` field returned by `GET /bim/group`.
