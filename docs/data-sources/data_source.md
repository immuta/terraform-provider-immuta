---
page_title: "immuta_data_source Data Source - terraform-provider-immuta"
subcategory: ""
description: |-
  Looks up an existing Immuta data source, such as a table.
---

# immuta_data_source (Data Source)

Looks up an existing Immuta data source, such as a table, with the documented
[data source endpoints](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/data-sources/get-data-sources):
by its exact name (`GET /dataSource/name/{name}`), or by the connection and
path it was registered from (`GET /dataSource`). Use it to give
[`immuta_domain_data_source_assignment`](../resources/domain_data_source_assignment.md)
a data source ID.

## Example usage

```terraform
# By the connection and path a table was registered from.
data "immuta_data_source" "orders" {
  connection_key = "acme_dev_databricks"
  path           = ["acme_dev", "sales", "orders"]
}

# Or by its exact name.
data "immuta_data_source" "customers" {
  name = "acme_dev_databricks . acme_dev . sales . customers"
}

output "orders_data_source_id" {
  value = data.immuta_data_source.orders.id
}
```

## Schema

### Optional

Set either `name`, or `connection_key` and `path`.

- `name` (String) Exact name of the data source.
- `connection_key` (String) Key of the connection the data source was
  registered from, usually `immuta_connection.<name>.connection_key`.
- `path` (List of String) Names from the top of the connection down to the
  table, for example `["acme_dev", "sales", "orders"]`. Case-sensitive.

### Read-Only

- `id` (String) ID of the data source.
- `data_object_id` (String) ID of the connection data object of the data
  source, as `immuta_data_object_settings.<name>.id`.

`name`, `connection_key` and `path` are also set, as Immuta holds them.

## Behaviour notes

- **By path,** the lookup searches data source names for the last path name
  (the table), then keeps the data source from that connection with exactly
  that path. Immuta's default names contain the table name; a data source
  renamed without it is only found by `name`. When nothing matches, the error
  lists the paths found in the connection.
- **Visibility:** only data sources the provider's user can see are found.
