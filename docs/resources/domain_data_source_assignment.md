---
page_title: "immuta_domain_data_source_assignment Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Adds one data source, such as a table, to a domain with manual assignment.
---

# immuta_domain_data_source_assignment (Resource)

Adds one data source, such as a table, to an [`immuta_domain`](domain.md) with
manual data source assignment, with the documented
[`POST /domain/{domainId}/datasources`](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/domains-api).
Destroying it removes the data source from the domain, with
`DELETE /domain/{domainId}/datasources/{dataSourceId}`.

This is the older, explicit alternative to
[`immuta_domain_dynamic_assignment`](domain_dynamic_assignment.md); a domain
uses one or the other. Find data source IDs with the
[`immuta_data_source`](../data-sources/data_source.md) data source.

## Example usage

```terraform
resource "immuta_domain" "sales" {
  name = "Sales"
}

# Look the table up by the connection and path it was registered from. The
# connection key is usually immuta_connection.<name>.connection_key.
data "immuta_data_source" "orders" {
  connection_key = "acme_dev_databricks"
  path           = ["acme_dev", "sales", "orders"]
}

resource "immuta_domain_data_source_assignment" "orders" {
  domain_id      = immuta_domain.sales.id
  data_source_id = data.immuta_data_source.orders.id
}

# Several tables: one assignment each.
data "immuta_data_source" "sales" {
  for_each = toset(["customers", "invoices"])

  connection_key = "acme_dev_databricks"
  path           = ["acme_dev", "sales", each.key]
}

resource "immuta_domain_data_source_assignment" "sales" {
  for_each = data.immuta_data_source.sales

  domain_id      = immuta_domain.sales.id
  data_source_id = each.value.id
}
```

## Schema

### Required

- `domain_id` (String) ID of the domain, usually `immuta_domain.<name>.id`.
  Changing it moves the data source.
- `data_source_id` (String) ID of the data source, usually
  `data.immuta_data_source.<name>.id`. Changing it assigns another data source.

### Read-Only

- `id` (String) `<domain_id>/<data_source_id>`.

## Behaviour notes

- **One resource per data source,** so that configurations never overwrite
  each other's assignments. Data sources added to the domain in other ways are
  left alone.
- **Waits for Immuta.** Immuta adds data sources in a background job; the
  provider checks every 2 seconds, for up to 5 minutes, until the data source is
  in the domain.
- **Manual domains only.** Immuta refuses to add data sources to a domain with
  an `immuta_domain_dynamic_assignment`, and refuses data sources already in
  another domain; the error says so.
- **Already in this domain** is an error that gives the `terraform import`
  command, so that Terraform never takes over, and later removes, an
  assignment it did not make.
- **Drift:** a data source removed from the domain in Immuta is removed from
  state and added again on the next apply.

## Import

Assignments are imported with `<domain_id>/<data_source_id>`.

```shell
# Assignments are imported with <domain_id>/<data_source_id>.
terraform import immuta_domain_data_source_assignment.orders c7458ee9-03e5-4b6f-8395-ad4e49235428/36
```
