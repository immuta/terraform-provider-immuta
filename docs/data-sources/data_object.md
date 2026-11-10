---
page_title: "immuta_data_object Data Source - terraform-provider-immuta"
subcategory: ""
description: |-
  Looks up a data object that a connection registered, by its path.
---

# immuta_data_object (Data Source)

Looks up a data object (a catalog, database, schema or table) that an
[`immuta_connection`](../resources/connection.md) registered, by its path. It uses the
documented [connections API](https://documentation.immuta.com/saas/developer-guides/api-intro/connections-api):
`POST /api/connections/dataObjects/_search`. Use its `id` wherever a data object
ID is needed, for example in
[`immuta_data_object_tag`](../resources/data_object_tag.md).

## Example usage

```terraform
# Look up a table a connection registered, by the names from the top of the
# connection down to it.
data "immuta_data_object" "orders" {
  connection_key = "acme_dev_databricks"
  path           = ["acme_dev", "sales", "orders"]
}

# Or a schema, or a catalog.
data "immuta_data_object" "sales" {
  connection_key = "acme_dev_databricks"
  path           = ["acme_dev", "sales"]
}

output "orders_id" {
  value = data.immuta_data_object.orders.id
}
```

## Schema

### Required

- `connection_key` (String) Key of the connection that holds the data object,
  usually `immuta_connection.<name>.connection_key`, so that the data object is
  looked up after the connection is registered.
- `path` (List of String) Names from the top of the connection down to the data
  object, for example `["acme_dev"]` for a catalog or
  `["acme_dev", "sales", "orders"]` for a table. The match is exact and
  case-sensitive.

### Read-Only

- `id` (String) ID of the data object.
- `name` (String) Name of the data object, the last element of `path`.
- `type` (String) Type of the data object, for example `CATALOG`, `SCHEMA` or
  `TABLE`.

## Behaviour notes

- **Waits for the crawl.** A connection registers its data objects
  asynchronously. When the data object is not there yet, the provider searches
  again every 5 seconds, for up to 10 minutes. If it is still missing, the error
  lists the data objects with the same name, to help fix the path. A wrong path
  therefore takes 10 minutes to fail.
