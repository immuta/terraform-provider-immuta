# The Entra group, crawled by the Entra connection. Its Immuta data object ID
# is bound, not its Azure object ID.
data "immuta_data_object" "analysts_group" {
  connection_key = "entra-prod"
  path           = ["IMMUTA_SNOWFLAKE_ANALYST"]
}

# A securable in a Custom connection, registered with immuta_connection.
resource "immuta_custom_data_object" "analyst_access" {
  connection_key = "snowflake-access"
  name           = "Analyst Access"
  type           = "SECURABLE"
}

# Destroying the binding unbinds the group. Immuta does not delete a bound
# securable, so Terraform destroys this binding before the securable.
resource "immuta_custom_securable_access_group" "analysts" {
  connection_key    = immuta_custom_data_object.analyst_access.connection_key
  securable_id      = immuta_custom_data_object.analyst_access.id
  identity_group_id = data.immuta_data_object.analysts_group.id
}
