---
page_title: "immuta_domain_data_sources Data Source - terraform-provider-immuta"
subcategory: ""
description: |-
  Lists the Immuta data sources in a domain.
---

# immuta_domain_data_sources (Data Source)

Lists the Immuta data sources in a domain, with the documented
[`GET /domain/{domainId}/datasources`](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/domains-api)
endpoint, following every page. Use it to give
[`immuta_data_product`](../resources/data_product.md) its `data_sources`.

## Example usage

```terraform
# Every data source in the domain. The domain id is usually
# immuta_domain.<name>.id.
data "immuta_domain_data_sources" "sales" {
  domain_id = "77408931-aa5a-440b-9c8a-4764a2c03411"
}

# Only the data sources whose name contains "orders".
data "immuta_domain_data_sources" "orders" {
  domain_id   = data.immuta_domain_data_sources.sales.domain_id
  search_text = "orders"
}

output "order_data_source_ids" {
  value = data.immuta_domain_data_sources.orders.ids
}
```

## Schema

### Required

- `domain_id` (String) ID of the domain, usually `immuta_domain.<name>.id`.

### Optional

- `search_text` (String) Only list the data sources whose name contains this
  text.

### Read-Only

- `ids` (List of String) IDs of the data sources, in the order Immuta returns
  them.
- `data_sources` (List of Object) The data sources, in the order Immuta
  returns them. Each has:
  - `id` (String) ID of the data source.
  - `name` (String) Name of the data source.
  - `platform` (String) Data platform of the data source.
