---
page_title: "immuta_user Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages a user in Immuta's built-in identity manager with the documented /bim API.
---

# immuta_user (Resource)

Manages a user in [Immuta's built-in identity manager](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/bim) (BIM).

Use this resource only for users Immuta itself holds. Users that come from an
external identity manager are synchronised by Immuta and must be managed
there, not here.

Managing users requires the `USER_ADMIN` permission.

~> **Destroying this resource permanently deletes the user.** See
[Destructive behaviour](#destructive-behaviour).

## Example usage

```terraform
resource "immuta_user" "jane" {
  user_id     = "jane.doe@immuta.com"
  name        = "Jane Doe"
  email       = "jane.doe@immuta.com"
  permissions = ["CREATE_PROJECT"]
}
```

## Schema

### Required

- `user_id` (String) Username of the user, usually an email address. Changing
  it replaces the user.

### Optional

- `name` (String) Display name of the user. The documented profile update
  endpoint does not accept a name, so changing it replaces the user.
- `email` (String) Email address on the user's profile. Immuta derives one
  from the username when it is not set, so removing the argument leaves the
  last value in place rather than clearing it.
- `phone` (String) Phone number on the user's profile.
- `sql_user` (String) The user's SQL username.
- `about` (String) Details about the user, displayed on their profile.
- `location` (String) The user's location.
- `organization` (String) The user's organization.
- `position` (String) The user's position.
- `permissions` (Set of String) Immuta permissions held by the user, for
  example `CREATE_PROJECT` or `USER_ADMIN`. When set, this is the complete
  set. When unset, permissions are not managed. Use `permissions = []` for a
  user with no permissions.
- `disabled` (Boolean) Whether the user is disabled. Defaults to `false`.

### Read-Only

- `id` (String) Numeric profile ID of the user, assigned by Immuta.
- `iam_id` (String) Identity manager the user belongs to, always `bim`.
- `created_at` (String) Timestamp of when the user was created.
- `updated_at` (String) Timestamp of when the user was last updated.

Group membership is managed with
[`immuta_group_user`](group_user.md) and attributes with
[`immuta_user_attribute`](user_attribute.md), so that memberships and
attributes owned elsewhere are not overwritten.

## Behaviour notes

- **When set, `permissions` is the complete set.** The documented
  `PUT /bim/iam/{iamid}/user/{userid}/permissions` endpoint replaces the whole
  list, so a permission granted in the Immuta UI is removed on the next apply.
- **Tenant default permissions are handled.** A tenant can grant permissions,
  such as `CREATE_PROJECT`, to every new user. When `permissions` is set, the
  provider writes the configured set again straight after creating the user,
  so those defaults are removed and the result matches the plan.
  `permissions = []` gives a user no permissions at all.
- **Unset means unmanaged.** When `permissions` is omitted, Terraform records
  whatever Immuta assigns, including tenant defaults, and never changes it.
  Removing `permissions` from an existing configuration stops managing it and
  leaves the current permissions in place rather than clearing them.
- **Only the username and name force replacement.** Profile fields,
  permissions and `disabled` are updated in place.
- **No password is managed.** The create endpoint returns a single-use login
  link, which the provider deliberately discards rather than writing to state.
  Users set their own password, or authenticate through your identity manager.
- **Remote changes show up as drift.** A profile field edited in the Immuta UI
  produces a diff on the next plan. A user deleted outside Terraform is
  removed from state and planned for recreation.

## Destructive behaviour

Destroying an `immuta_user` **permanently deletes the user** with
`DELETE /bim/iam/{iamid}/user/{userid}`. The deletion cannot be undone.

~> **This endpoint is not in the published Immuta API reference.** Immuta
documents permanent deletion only as a UI action. The provider issues the same
request the Immuta UI issues, because the documented alternative, disabling
the user, leaves the account in place and makes `terraform destroy`
misleading. It is the only undocumented call in the provider and it therefore
carries no compatibility guarantee from Immuta.

To keep the account instead of deleting it, set `disabled = true` and then
remove the resource from state without destroying it:

```sh
terraform state rm immuta_user.jane
```

Deleting a user removes their group memberships and attributes with them.
Anything that depended on those, including subscriptions granted by
attribute-based policies, stops applying. Plan user removal in a sandbox
tenant before doing it anywhere that matters.

## Testing this resource

```sh
export IMMUTA_HOST="https://sandbox.immuta.com"
export IMMUTA_API_KEY="..."

# Terraform independent check of connectivity, credentials and permissions.
make smoke-bim

# Full create, no-op plan, update, import and destroy cycle, with every step
# verified against a direct API read.
make testacc
```

Both commands create throwaway users and clean up after themselves. Never
point them at a production tenant.

## Import

Users are imported with the numeric profile ID Immuta assigned them,
optionally prefixed with the IAM id.

```shell
terraform import immuta_user.jane 18
terraform import immuta_user.jane bim/18
```

The numeric ID is the `id` field returned by `GET /bim/user`.
