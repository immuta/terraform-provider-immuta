---
page_title: "immuta_connection Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Registers an Immuta connection to a data platform, or to an Entra or Okta identity provider, with the connections API.
---

# immuta_connection (Resource)

Registers an Immuta connection to a data platform (BigQuery, Databricks,
Redshift, Snowflake, Teradata or Trino), or to an Entra or Okta identity
provider, with the
[connections API](https://documentation.immuta.com/saas/developer-guides/api-intro/connections-api).

The connection's settings are kept as a YAML (or JSON) document in the format
of the API's registration payload, so a payload captured from the Immuta UI or
taken from the API reference can be used as it is. The credential is a
separate, sensitive document, so that secrets come from variables and never
appear in plans or in the settings files.

The Immuta user behind the API key needs the `APPLICATION_ADMIN` permission.

~> **Destroying or replacing a connection deletes it, and every data object
registered through it, in Immuta.** Use `lifecycle { prevent_destroy = true }`
as in the example below.

## Example usage

```terraform
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
```

`connections/trino.yaml.tftpl`, a template so that the owner comes from
`data.immuta_user` instead of a hard-coded profile ID:

```yaml
# Trino connection captured in example_queries.md, without its credential,
# which is set with the credential attribute. technology (Trino) and
# connectionKey (test) are set with attributes too.
clientConfiguration:
  host: "localhost"
  port: 100
  ssl: false
  ignoreCertValidation: true
dataObjectSync:
  enabled: true
  startTime: 0
  schedule: "EVERY_24_HOURS"
syncOnCreate: true
dataObjectDefaults:
  syncPolicy: false
  # Immuta user profile ID, filled in by templatefile() from data.immuta_user.
  owners:
    - type: "USER"
      id: ${owner_id}
```

The [examples directory](https://github.com/immuta/terraform-provider-immuta/tree/main/examples/resources/immuta_connection/connections)
also holds Databricks, Snowflake, Entra and Okta configurations.

## Identity connections (Entra and Okta)

An Entra or Okta connection crawls the identity provider's groups whose names
start with `clientConfiguration.immutaResourcePrefix`. When a policy or an
access request grants access to a securable bound to one of those groups,
Immuta adds or removes the user in the group through that connection.

`connections/okta.yaml.tftpl`:

```yaml
# Okta identity connection. Its credential is set with the credential
# attribute, and technology (Okta) and connectionKey with attributes too.
clientConfiguration:
  # The hostname only, without a scheme, port or path.
  oktaDomain: "${okta_domain}"
  # Only groups whose name starts with this prefix are crawled.
  immutaResourcePrefix: "IMMUTA_"
# Only the prefix filter is supported, and it takes no configuration. Both
# keys are top-level: Immuta rejects them in clientConfiguration.
objectSyncFilterType: "prefix"
objectSyncFilterConfiguration: {}
dataObjectSync:
  enabled: true
  startTime: 0
  schedule: "EVERY_24_HOURS"
syncOnCreate: true
```

| Setting | Entra | Okta |
| --- | --- | --- |
| `clientConfiguration` | `directoryTenantId`, `immutaResourcePrefix` | `oktaDomain` (hostname only, such as `dev-123456.okta.com`), `immutaResourcePrefix` |
| Top level of the configuration | `objectSyncFilterType: prefix` and an empty `objectSyncFilterConfiguration` (Immuta rejects them in `clientConfiguration`) | The same |
| `credential` | `OAuthWithSecret` (OAuth M2M with a client secret): `clientId`, `clientSecret`, `authorityUrl` (`https://login.microsoftonline.com/<tenant ID>/oauth2/v2.0/token`), `scope` (`https://graph.microsoft.com/.default`) | `Token` with `token` (an API token), or `OAuthWithSecret` with `clientId`, `clientSecret`, `authorityUrl` (`https://<Okta domain>/oauth2/v1/token`) and `scope` (`okta.groups.read`) |

The Entra app registration needs Microsoft Graph application permissions to
read groups and manage their members.

The provider rejects an `oktaDomain` with a scheme, port or path at plan time.
No identity connection setting replaces the connection when it changes: every
change is sent as an update. Destroying an identity connection deletes the
groups crawled through it.

## Schema

### Required

- `connection_key` (String) Unique key of the connection. It becomes the name
  of the top-level data object in Immuta. Changing it replaces the connection.
- `technology` (String) `BigQuery`, `Databricks`, `Entra`, `Okta`, `Redshift`,
  `Snowflake`, `Teradata` or `Trino`. Changing it replaces the connection.
- `configuration` (String) The rest of the registration payload, as a YAML or
  JSON document: `clientConfiguration` (without its `credential`),
  `dataObjectSync`, `dataObjectDefaults`, `policyConfiguration`,
  `auditConfiguration` and `syncOnCreate`, and for Entra and Okta
  `objectSyncFilterType` and `objectSyncFilterConfiguration`.
- `credential` (String, Sensitive) The `clientConfiguration.credential` of the
  payload, as a YAML or JSON document, usually built with `jsonencode()` from
  variables. It needs an `authenticationType`.

### Read-Only

- `id` (String) The connection key.
- `state` (String) State of the connection in Immuta, such as `ACTIVE` or
  `UNHEALTHY`, read when the resource was last applied or refreshed.

## Behaviour notes

- **Formatting is not a change.** Comments, indentation, key order, and YAML
  versus JSON never produce a diff, in either document.
- **Only what changed is sent.** An update sends the changed settings with
  `PATCH /api/connections/{connectionKey}`, and nothing at all when nothing
  Immuta can update has changed.
- **The credential is only sent when it changes**, because Immuta crawls the
  connection again whenever it receives one.
- **Some settings cannot be changed in place.** The connections API cannot
  update the settings below, so changing one replaces the connection, which
  deletes its data objects. The plan shows a warning naming the setting.

  | Technology | Settings |
  | --- | --- |
  | All | `connection_key`, `technology` |
  | BigQuery | `clientConfiguration.projectId`, `location`, `immutaDataset`, `directoryCustomerId` |
  | Databricks | `clientConfiguration.host`, `port`, `immutaSystemCatalog`; `policyConfiguration.excludeGroupPattern` |
  | Redshift | `clientConfiguration.host`, `port`, `immutaSystemDatabase` |
  | Snowflake | `clientConfiguration.host`, `port`, `immutaSystemDatabase` |

- **`syncOnCreate` only applies when the connection is registered.** Changing
  it later does nothing.
- **Removing a setting leaves its value in Immuta.** Set the value you want
  instead of deleting the line.
- **Drift detection covers configured settings.** A configured setting
  changed in Immuta shows as a diff and is put back on the next apply. Settings
  you do not configure, defaults Immuta fills in, and fields such as `state`
  and `health` are ignored. Immuta never returns secrets, so a credential
  changed outside Terraform is not detected.
- **The provider waits for Immuta.** After registering or updating a
  connection it waits up to 10 minutes for it to leave `ACTIVATING` or
  `UPDATING`. An `UNHEALTHY` connection is saved and reported as a warning, so
  it can be fixed in place, for example with a corrected credential.
- **Deletion is asynchronous.** The provider waits up to 15 minutes for the
  connection to be gone, so that a replacement can reuse its key. Teardown
  instructions returned by Immuta are shown as a warning.

## Import

Connections are imported with their connection key. The import starts from
the settings Immuta returns, and only the settings in `configuration` are
compared from then on. Immuta never returns secrets, so the first apply after
an import sends the configured credential, which makes Immuta crawl the
connection again.

```shell
terraform import immuta_connection.trino trino
```
