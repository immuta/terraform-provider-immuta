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

# Identity connections. Entra and Okta crawl the identity provider's groups
# that start with immutaResourcePrefix. Access is then granted through
# membership of those groups, which Immuta manages in the identity provider.
locals {
  entra_tenant_id = "00000000-0000-0000-0000-000000000000"
  okta_domain     = "dev-123456.okta.com" # hostname only
}

variable "entra_client_secret" {
  type      = string
  sensitive = true
}

variable "okta_api_token" {
  type      = string
  sensitive = true
}

resource "immuta_connection" "entra" {
  connection_key = "entra-prod"
  technology     = "Entra"
  configuration  = templatefile("${path.module}/connections/entra.yaml.tftpl", { tenant_id = local.entra_tenant_id })

  # OAuth M2M (client secret) for an app registration with the Microsoft
  # Graph application permissions to read groups and manage their members.
  credential = jsonencode({
    authenticationType = "OAuthWithSecret"
    clientId           = "11111111-1111-1111-1111-111111111111"
    authorityUrl       = "https://login.microsoftonline.com/${local.entra_tenant_id}/oauth2/v2.0/token"
    scope              = "https://graph.microsoft.com/.default"
    clientSecret       = var.entra_client_secret
  })

  # Destroying an identity connection deletes the groups crawled through it.
  lifecycle {
    prevent_destroy = true
  }
}

resource "immuta_connection" "okta" {
  connection_key = "okta-prod"
  technology     = "Okta"
  configuration  = templatefile("${path.module}/connections/okta.yaml.tftpl", { okta_domain = local.okta_domain })

  # An Okta API token. To use OAuth client credentials instead, set
  # authenticationType = "OAuthWithSecret" with clientId, clientSecret,
  # authorityUrl = "https://${local.okta_domain}/oauth2/v1/token" and
  # scope = "okta.groups.read".
  credential = jsonencode({
    authenticationType = "Token"
    token              = var.okta_api_token
  })

  lifecycle {
    prevent_destroy = true
  }
}

# A Custom connection holds folders and securables created in Immuta rather
# than crawled from a platform (see immuta_custom_data_object). It takes no
# credential.
resource "immuta_connection" "snowflake_access" {
  connection_key = "snowflake-access"
  technology     = "Custom"
  configuration  = templatefile("${path.module}/connections/custom.yaml.tftpl", local.connection_template_vars)
}
