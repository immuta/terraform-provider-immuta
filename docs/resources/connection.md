---
page_title: "immuta_connection Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Registers an Immuta connection to a data platform with the connections API.
---

# immuta_connection (Resource)

Registers an Immuta connection to a data platform (BigQuery, Databricks,
Redshift, Snowflake, Teradata or Trino) with the
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
also holds Databricks and Snowflake configurations.

## Schema

### Required

- `connection_key` (String) Unique key of the connection. It becomes the name
  of the top-level data object in Immuta. Changing it replaces the connection.
- `technology` (String) `BigQuery`, `Databricks`, `Redshift`, `Snowflake`,
  `Teradata` or `Trino`. Changing it replaces the connection.
- `configuration` (String) The rest of the registration payload, as a YAML or
  JSON document: `clientConfiguration` (without its `credential`),
  `dataObjectSync`, `dataObjectDefaults`, `policyConfiguration`,
  `auditConfiguration` and `syncOnCreate`.
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
