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
