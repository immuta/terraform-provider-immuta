# Folders and securables live in a Custom connection, registered with
# immuta_connection (technology = "Custom"). Its key is used directly here.
locals {
  custom_connection_key = "snowflake-access"
}

# A folder at the top of the connection. Destroying or replacing it deletes
# everything under it in Immuta.
resource "immuta_custom_data_object" "analytics" {
  connection_key = local.custom_connection_key
  name           = "Analytics"
  type           = "FOLDER"
}

# A securable in that folder. Users are granted access to the securable; bind
# an identity group to it with immuta_custom_securable_access_group.
resource "immuta_custom_data_object" "analyst_access" {
  connection_key = local.custom_connection_key
  parent_id      = immuta_custom_data_object.analytics.id
  name           = "Analyst Access"
  type           = "SECURABLE"
}
