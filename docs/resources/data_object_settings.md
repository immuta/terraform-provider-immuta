---
page_title: "immuta_data_object_settings Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages the settings of a data object that a connection registered.
---

# immuta_data_object_settings (Resource)

Manages the settings of a data object (a catalog, database, schema or table)
that an [`immuta_connection`](connection.md) registered. Today that is the
"Data policy" setting, `syncPolicy`. It uses the documented
[connections API](https://documentation.immuta.com/saas/developer-guides/api-intro/connections-api):
`POST /api/connections/dataObjects/_search` to find the data object, and
`GET` and `PATCH /api/connections/{connectionKey}/dataObjects/{id}`.

The data object belongs to the connection: this resource never creates or
deletes it.

## Example usage

```terraform
# Turn on "Data policy" for data objects a connection registered. The
# connection key is usually immuta_connection.<name>.connection_key, so that
# the data objects are looked up once the connection is registered.
locals {
  connection_key = "acme_dev_databricks"
}

# A catalog: the schemas and tables under it inherit the setting.
resource "immuta_data_object_settings" "acme_dev" {
  connection_key = local.connection_key
  path           = ["acme_dev"]
  sync_policy    = true
}

# Set it on each schema and table too, rather than relying on inheritance.
resource "immuta_data_object_settings" "sales" {
  for_each = {
    schema = ["acme_dev", "sales"]
    orders = ["acme_dev", "sales", "orders"]
  }

  connection_key = local.connection_key
  path           = each.value
  sync_policy    = true
}

# Or turn it off for one table.
resource "immuta_data_object_settings" "payroll" {
  connection_key = local.connection_key
  path           = ["acme_dev", "hr", "payroll"]
  sync_policy    = false
}
```

## Schema

### Required

- `connection_key` (String) Key of the connection that holds the data object,
  usually `immuta_connection.<name>.connection_key`, so that the data object
  is looked up after the connection is registered. Changing it manages another
  data object.
- `path` (List of String) Names from the top of the connection down to the
  data object, for example `["acme_dev"]` for a catalog or
  `["acme_dev", "sales", "orders"]` for a table. Case-sensitive. Changing it
  manages another data object.
- `sync_policy` (Boolean) Whether Immuta applies data policies to the data
  object (the "Data policy" setting).

### Read-Only

- `id` (String) ID of the data object.
- `type` (String) Type of the data object, for example `CATALOG`, `SCHEMA` or
  `TABLE`.
- `sync_policy_inherited` (Boolean) Whether Immuta reports `sync_policy` as
  inherited from a parent data object or the connection rather than set on
  this data object.

## Behaviour notes

- **Waits for the crawl.** A connection registers its data objects
  asynchronously. When the data object is not there yet, the provider searches
  again every 5 seconds for up to 10 minutes. If it is still missing, the error
  lists the data objects with the same name, to help fix the path.
- **Inheritance.** A setting on a catalog, database or schema applies to the
  objects under it that do not set their own. This resource sets the value on
  its data object; use one per level to set every level explicitly.
- **Only `syncPolicy` is sent.** Owners and other settings are left as they
  are.
- **Drift is detected** when the effective value differs from the
  configuration, and is put back on the next apply.
- **Destroy clears the setting** (`syncPolicy: null`), so that the data object
  inherits it again.
- **A data object that disappears** (dropped and synced) is removed from
  state. If it is crawled again, possibly with a new ID, the next apply finds
  it by path and sets it again.

## Import

Data object settings are imported with `<connection_key>/<data object id>`.

```shell
# Data object settings are imported with <connection_key>/<data object id>.
terraform import immuta_data_object_settings.payroll acme_dev_databricks/e6cb340a-4a44-42a5-bed3-0ffe1c491926
```
