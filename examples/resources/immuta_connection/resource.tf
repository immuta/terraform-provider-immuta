# Each connection's settings live in a YAML template in connections/. Secrets
# never go in those files: they are passed in with the sensitive credential
# attribute, here from variables.

# The owner of the data objects each connection registers, looked up by email
# so that no profile ID is hard-coded.
data "immuta_user" "data_owner" {
  email = "data.owner@example.com"
}

locals {
  connection_template_vars = {
    owner_id = data.immuta_user.data_owner.id
  }
}

variable "trino_password" {
  type      = string
  sensitive = true
}

variable "databricks_token" {
  type      = string
  sensitive = true
}

variable "snowflake_client_secret" {
  type      = string
  sensitive = true
}

resource "immuta_connection" "trino" {
  connection_key = "trino"
  technology     = "Trino"
  configuration  = templatefile("${path.module}/connections/trino.yaml.tftpl", local.connection_template_vars)

  credential = jsonencode({
    authenticationType = "UsernamePassword"
    username           = "immuta_service"
    password           = var.trino_password
  })

  # Destroying a connection deletes all of its data objects in Immuta.
  lifecycle {
    prevent_destroy = true
  }
}

resource "immuta_connection" "databricks" {
  connection_key = "databricks"
  technology     = "Databricks"
  configuration  = templatefile("${path.module}/connections/databricks.yaml.tftpl", local.connection_template_vars)

  credential = jsonencode({
    authenticationType = "Token"
    token              = var.databricks_token
  })

  lifecycle {
    prevent_destroy = true
  }
}

resource "immuta_connection" "snowflake" {
  connection_key = "snowflake"
  technology     = "Snowflake"
  configuration  = templatefile("${path.module}/connections/snowflake.yaml.tftpl", local.connection_template_vars)

  credential = jsonencode({
    authenticationType = "OAuthWithSecret"
    clientId           = "immuta-service"
    authorityUrl       = "https://login.example.com/oauth2/token"
    scope              = "session:role:IMMUTA_SYSTEM_ACCOUNT"
    clientSecret       = var.snowflake_client_secret
  })

  lifecycle {
    prevent_destroy = true
  }
}
