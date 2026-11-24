---
page_title: "immuta_group_attribute Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Assigns one attribute value to an Immuta group with the documented authorizations API.
---

# immuta_group_attribute (Resource)

Assigns one attribute value to an [Immuta group](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/bim)
with `PUT /bim/iam/{iamid}/group/{groupId}/authorizations/{name}/{value}`.

Every member of the group inherits the attribute, so this is usually the
right place to grant an attribute to a team rather than assigning it to each
user.

Managing attributes requires the `USER_ADMIN` permission.

## Example usage

```terraform
resource "immuta_group_attribute" "engineering_department" {
  group_id = immuta_group.engineering.id
  name     = "Department"
  value    = "Engineering"
}
```

## Schema

### Required

- `group_id` (Number) Numeric ID of the group, for example
  `immuta_group.engineering.id`. Changing it replaces the assignment.
- `name` (String) Name of the attribute, for example `Department`. Changing it
  replaces the assignment.
- `value` (String) Value of the attribute, for example `Engineering`. Changing
  it replaces the assignment.

### Optional

- `iam_id` (String) Identity manager the group belongs to. Defaults to `bim`.
  Changing it replaces the assignment.

### Read-Only

- `id` (String) Identifier of the assignment, in the form
  `{iam_id}/{group_id}/{name}/{value}`.

## Behaviour notes

- **Nothing is updated in place.** Every argument replaces the assignment,
  because assigning and removing an attribute are the only documented
  operations.
- **Remote changes show up as drift.** An attribute removed in the Immuta UI
  is removed from state and planned for reassignment.
- **Attributes assigned outside Terraform are left alone.** The resource only
  ever adds and removes the one name and value pair it manages.

## Destructive behaviour

Destroying an `immuta_group_attribute` removes that one attribute value from
the group, and therefore from every member of the group at once. The group
and its members are not deleted.

This is the widest blast radius of the identity resources: one small change
can revoke access for everyone in the group. Check which policies reference
the attribute, and try it in a sandbox tenant first.

## Testing this resource

```sh
export IMMUTA_HOST="https://sandbox.immuta.com"
export IMMUTA_API_KEY="..."

make smoke-bim
make testacc
```

## Import

Assignments are imported with `{iam_id}/{group_id}/{name}/{value}`.

```shell
terraform import immuta_group_attribute.engineering_department 'bim/12/Department/Engineering'
```

Attribute names and values that contain a `/` cannot be imported, because the
import ID uses `/` as its separator.
