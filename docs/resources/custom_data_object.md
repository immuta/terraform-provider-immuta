---
page_title: "immuta_custom_data_object Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Creates a folder or a securable under a Custom connection.
---

# immuta_custom_data_object (Resource)

Creates a folder or a securable under a `Custom` connection, with the
documented connections API
(`POST /api/connections/{connectionKey}/dataObjects`).

A `Custom` connection, registered with
[`immuta_connection`](connection.md) and `technology = "Custom"`, holds data
objects created in Immuta rather than crawled from a platform. A securable is
what users get access to; bind an identity group to it with
[`immuta_custom_securable_access_group`](custom_securable_access_group.md). A
folder groups securables and other folders.

**Destroying or replacing a folder deletes it and everything under it in
Immuta.**

## Example usage

```terraform
# Folders and securables live in a Custom connection, registered with
# immuta_connection (technology = "Custom"). Its key is used directly here.
locals {
  custom_connection_key = "snowflake-access"
}

# A folder at the top of the connection. Destroying or replacing it deletes
# everything under it in Immuta.
resource "immuta_custom_data_object" "analytics" {
  connection_key = local.custom_connection_key
  name           = "Analytics"
  type           = "FOLDER"
}

# A securable in that folder. Users are granted access to the securable; bind
# an identity group to it with immuta_custom_securable_access_group.
resource "immuta_custom_data_object" "analyst_access" {
  connection_key = local.custom_connection_key
  parent_id      = immuta_custom_data_object.analytics.id
  name           = "Analyst Access"
  type           = "SECURABLE"
}
```

## Schema

### Required

- `connection_key` (String) Key of the `Custom` connection, usually `immuta_connection.<name>.connection_key`. Changing it replaces the data object.
- `name` (String) Name of the data object. It must be unique, for its type, under its parent. Changing it renames the data object in place. It must not be empty or start or end with whitespace.
- `type` (String) `FOLDER` or `SECURABLE`. Changing it replaces the data object.

### Optional

- `parent_id` (String) ID of the folder to create the data object in, usually `immuta_custom_data_object.<folder>.id`. Leave it out to create the data object at the top of the connection. Changing it replaces the data object.

### Read-Only

- `id` (String) ID of the data object, the securable or folder ID used by other resources.

## Behaviour notes

- **Renames are asynchronous.** Changing `name` renames the data object in place, and the provider waits, for up to 10 minutes, until Immuta reports the new name.
- **Deletes are asynchronous.** Destroying the data object deletes it (for a folder, with everything under it), and the provider waits, for up to 10 minutes, until Immuta no longer finds it. A replacement can therefore reuse the same name straight away.
- **Immuta does not delete a securable while a group is bound to it**, or while access Immuta authored for granted users remains, nor a folder that holds such a securable. The delete fails with a 409; for a securable, the error names the group that `GET /api/connections/{connectionKey}/dataObjects/{id}/accessGroup` reports. Destroy the [`immuta_custom_securable_access_group`](custom_securable_access_group.md) binding, or remove it in Immuta, then apply again. When both are in the same configuration, Terraform unbinds the group first.
- **Drift**: a data object deleted outside Terraform is removed from state and planned for recreation. A name changed in Immuta is planned as a rename back.
- **Parent**: the parent folder is read back from Immuta, which also fills in `parent_id` after an import. A data object at the top of the connection keeps `parent_id` unset.
- **Conflicts on create**: Immuta answers 409 when a data object with the same name and type already exists under the same parent. Import it rather than creating it again.

## Import

```shell
# A Custom data object is imported with <connection_key>/<data_object_id>.
terraform import immuta_custom_data_object.analyst_access snowflake-access/7d1d0c1e-5b0e-4a77-9a55-3b0f1c2d3e4f
```
