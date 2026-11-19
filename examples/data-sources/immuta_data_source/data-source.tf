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
