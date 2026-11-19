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
