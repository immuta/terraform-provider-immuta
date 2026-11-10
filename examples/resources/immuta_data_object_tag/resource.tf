# Tags managed by Terraform.
resource "immuta_tag" "gold" {
  name = "Access.Layer.Gold"
}

# A tag Terraform does not manage, created in the Immuta UI or by Immuta.
data "immuta_tag" "pii" {
  name = "Discovered.PII"
}

# The data object to tag, found by its path in the connection. The connection
# key is usually immuta_connection.<name>.connection_key.
data "immuta_data_object" "orders" {
  connection_key = "acme_dev_databricks"
  path           = ["acme_dev", "sales", "orders"]
}

# One resource per tag on a data object.
resource "immuta_data_object_tag" "orders_gold" {
  connection_key = data.immuta_data_object.orders.connection_key
  data_object_id = data.immuta_data_object.orders.id
  tag_id         = immuta_tag.gold.id
}

resource "immuta_data_object_tag" "orders_pii" {
  connection_key = data.immuta_data_object.orders.connection_key
  data_object_id = data.immuta_data_object.orders.id
  tag_id         = data.immuta_tag.pii.id
}
