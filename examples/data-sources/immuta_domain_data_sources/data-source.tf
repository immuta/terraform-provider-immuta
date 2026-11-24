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
