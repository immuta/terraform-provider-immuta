---
page_title: "immuta_custom_securable_access_group Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Binds an Entra or Okta identity group to a Custom securable.
---

# immuta_custom_securable_access_group (Resource)

Binds an identity group, crawled by an Entra or Okta connection, to a Custom
securable (`PUT /api/connections/{connectionKey}/dataObjects/{securableId}/accessGroup`
with `{"action": "bind"}`).

The binding itself grants nobody access. When a subscription policy or an
approved access request grants a user the securable, Immuta adds the user to
the group through the identity connection, and removes them when access is
revoked.

A securable has at most one bound group, and a group can be bound to only one
securable.

Destroying this resource unbinds the group
(`DELETE /api/connections/{connectionKey}/dataObjects/{securableId}/accessGroup`).
A binding Immuta no longer finds is treated as already gone.

**Immuta does not delete a securable while a group is bound to it** (or while
access Immuta authored for granted users remains).

- Destroying the securable together with this resource works: the binding
  references the securable, so Terraform unbinds the group first.
- Destroying the securable, or a folder holding it, while a group is bound
  outside Terraform fails with a 409. The error names the group that
  `GET /api/connections/{key}/dataObjects/{id}/accessGroup` reports.
- Changing `identity_group_id` replaces the binding: the old group is unbound,
  then the new one bound.
- Removing this resource with a `removed` block and `destroy = false` leaves
  the group bound.

## Example usage

```terraform
# The Entra group, crawled by the Entra connection. Its Immuta data object ID
# is bound, not its Azure object ID.
data "immuta_data_object" "analysts_group" {
  connection_key = "entra-prod"
  path           = ["IMMUTA_SNOWFLAKE_ANALYST"]
}

resource "immuta_custom_data_object" "analyst_access" {
  connection_key = immuta_connection.snowflake_access.connection_key
  name           = "Analyst Access"
  type           = "SECURABLE"
}

# Destroying the binding unbinds the group. Immuta does not delete a bound
# securable, so Terraform destroys this binding before the securable.
resource "immuta_custom_securable_access_group" "analysts" {
  connection_key    = immuta_custom_data_object.analyst_access.connection_key
  securable_id      = immuta_custom_data_object.analyst_access.id
  identity_group_id = data.immuta_data_object.analysts_group.id
}
```

## Schema

### Required

- `connection_key` (String) Key of the Custom connection that holds the securable, usually `immuta_custom_data_object.<name>.connection_key`. Changing it replaces the binding.
- `securable_id` (String) ID of the Custom securable, usually `immuta_custom_data_object.<name>.id`. Changing it replaces the binding.
- `identity_group_id` (String) Immuta data object ID of the identity group, usually `data.immuta_data_object.<name>.id` looked up in the Entra or Okta connection. It is not the group's ID in the identity provider. Changing it replaces the binding.

### Read-Only

- `id` (String) `<connection_key>/<securable_id>`.

## Behaviour notes

- **Create** binds the group. If the same group is already bound, for example after the resource was removed from the state, it is taken over. If another group is bound, create fails rather than replace a binding Terraform does not manage: import it, or remove it in Immuta, first.
- **Drift**: if Immuta reports another bound group, the plan replaces the binding, unbinding that group and binding the configured one. If no group is bound, for example because the binding was removed in Immuta, the binding is created again.
- **Delete** unbinds the group.

## Import

```shell
# A binding is imported with <connection_key>/<securable_id>.
terraform import immuta_custom_securable_access_group.analysts snowflake-access/7d1d0c1e-5b0e-4a77-9a55-3b0f1c2d3e4f
```
