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
