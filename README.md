# Terraform Provider for Immuta

Manage your [Immuta](https://www.immuta.com) SaaS tenant as code: domains,
users and groups, tags and classification, policies, connections and data
products.

> [!WARNING]
> **This provider is not part of the Immuta product and is not supported by
> Immuta Support.** It is provided "as is", without warranty of any kind,
> under the [Apache 2.0 licence](LICENSE). You use it at your own risk.
>
> The provider is pre-release, and its behaviour may change between versions.
> Try it against a non-production tenant first, and review every
> `terraform plan` before you apply it.
>
> **Please do not contact Immuta Support about this provider.** Report
> problems as [GitHub issues](#reporting-issues) instead.

## Getting started

### 1. Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) 1.0 or
  later. Provider functions need Terraform 1.8 or later.
- An Immuta SaaS tenant and an
  [API key](https://documentation.immuta.com/saas/developer-guides/api-intro/authentication)
  for a user with the permissions needed for the objects you want to manage.
  For example, managing domains requires `GOVERNANCE`.

### 2. Install the provider

The provider is not published to the Terraform Registry. Install it from
[GitHub Releases](https://github.com/immuta/terraform-provider-immuta/releases)
into Terraform's local plugin directory instead:

1. Download the `.zip` asset of the release you want, for example
   `v0.1.0-alpha`.
2. Unzip it into the plugin directory for that version:

   ```sh
   VERSION=0.1.0-alpha   # the release tag without the leading "v"
   DIR="$HOME/.terraform.d/plugins/registry.terraform.io/immuta/immuta/$VERSION/darwin_arm64"

   mkdir -p "$DIR"
   unzip -o ~/Downloads/terraform-provider-immuta_*.zip -d "$DIR"

   # The binary is not signed, so stop macOS from blocking it.
   xattr -dr com.apple.quarantine "$DIR"
   ```

If your `~/.terraformrc` already has a `provider_installation` block, add a
`filesystem_mirror` for `~/.terraform.d/plugins` to it, because Terraform
only checks that directory automatically when no such block exists.

### 3. Configure credentials

The provider uses two sets of credentials. Set them as environment variables,
so that they never end up in your configuration or in version control.

**Immuta API key (always required).** Generate it on your
Immuta profile page
([how to](https://documentation.immuta.com/saas/developer-guides/api-intro/authentication)):

```sh
export IMMUTA_HOST="https://your-tenant.immuta.com"
export IMMUTA_API_KEY="..."
```

**Request app token (data products only).** `immuta_data_product`,
`immuta_request_form` and `immuta_global_user` use the Request app (formerly
Marketplace) instead. It is reached through a regional URL and authenticated
with a personal access token, which you generate on the Developer Settings
page. Skip this if you do not manage data products:

```sh
# Use the URL for your tenant's region: na, eu or ap.
export IMMUTA_REQUEST_APP_URL="https://na.api.immutacloud.com/marketplace"
export IMMUTA_REQUEST_APP_TOKEN="..."
```

For all the provider settings, see the
[provider documentation](docs/index.md#authentication).

### 4. Write and apply a configuration

Pre-release versions must be pinned exactly, so set `version` to the release
you installed:

```terraform
terraform {
  required_providers {
    immuta = {
      source  = "immuta/immuta"
      version = "0.1.0-alpha"
    }
  }
}

provider "immuta" {} # reads IMMUTA_HOST and IMMUTA_API_KEY

resource "immuta_domain" "research" {
  name        = "Research"
  description = "Data owned by the research organisation"
}

# Put every data source tagged Discovered.Country.Denmark into the domain.
resource "immuta_domain_dynamic_assignment" "research" {
  domain_id = immuta_domain.research.id
  tags      = ["Discovered.Country.Denmark"]
}
```

```sh
terraform init
terraform plan
terraform apply
```

To upgrade, install the new release in the same way, update `version`, and
run `terraform init -upgrade`.

There are more examples in [`examples/`](examples) and a full reference for
each resource in [`docs/`](docs).

## What you can manage

### Domains

A domain groups data sources by business area, so that governance can be
delegated to the people who own that data.

| Resource / data source | What it does |
| --- | --- |
| [`immuta_domain`](docs/resources/domain.md) | Creates a domain and controls its autoscanning setting |
| [`immuta_domain_dynamic_assignment`](docs/resources/domain_dynamic_assignment.md) | Adds data sources to a domain automatically, by tag |
| [`immuta_domain_data_source_assignment`](docs/resources/domain_data_source_assignment.md) | Adds a single data source, such as a table, to a domain |
| [`immuta_domain_data_sources`](docs/data-sources/domain_data_sources.md) (data) | Lists the data sources in a domain |

### Users, groups and attributes

Users, groups and their attributes control who can access which data. These
resources manage Immuta's built-in identity manager. Users from an external
identity provider can be looked up instead.

| Resource / data source | What it does |
| --- | --- |
| [`immuta_user`](docs/resources/user.md) | Creates a user |
| [`immuta_group`](docs/resources/group.md) | Creates a group |
| [`immuta_group_user`](docs/resources/group_user.md) | Adds a user to a group |
| [`immuta_user_attribute`](docs/resources/user_attribute.md) | Gives a user an attribute value, such as `Department: Finance` |
| [`immuta_group_attribute`](docs/resources/group_attribute.md) | Gives a group an attribute value |
| [`immuta_user`](docs/data-sources/user.md) (data) | Looks up an existing user by email or username |
| [`immuta_global_user`](docs/data-sources/global_user.md) (data) | Looks up a user's global ID, which data products need |

### Tags and classification

Tags describe what data is, such as a country or a sensitivity level, and
policies refer to them. Identifiers and frameworks apply tags automatically.

| Resource / data source | What it does |
| --- | --- |
| [`immuta_tag`](docs/resources/tag.md) | Creates a tag |
| [`immuta_identifier`](docs/resources/identifier.md) | Defines a pattern that sensitive data discovery uses to tag columns |
| [`immuta_domain_identifier`](docs/resources/domain_identifier.md) | Copies an identifier into a domain |
| [`immuta_framework`](docs/resources/framework.md) | Defines a classification framework: rules that derive tags, such as sensitivity, from other tags |
| [`immuta_tag`](docs/data-sources/tag.md) (data) | Looks up an existing tag |
| `provider::immuta::decode_framework_rules` ([function](docs/functions/decode_framework_rules.md)) | Validates and decodes framework rules written in YAML |

### Policies

Global policies apply across data sources by tag and user attribute,
instead of being set table by table.

| Resource | What it does |
| --- | --- |
| [`immuta_global_policy`](docs/resources/global_policy.md) | Creates a data policy (such as masking or row filtering) or a subscription policy (who can access data), written as a YAML or JSON document |

### Connections and data objects

A connection registers a data platform, such as Snowflake, Databricks or
Redshift, with Immuta. Immuta then discovers the platform's catalogs,
schemas and tables, which are its data objects. A connection can also
register an Entra or Okta identity provider, whose groups Immuta crawls and
manages memberships of.

| Resource / data source | What it does |
| --- | --- |
| [`immuta_connection`](docs/resources/connection.md) | Registers a connection from a YAML configuration document |
| [`immuta_data_object_settings`](docs/resources/data_object_settings.md) | Turns data policy on or off for a registered catalog, schema or table |
| [`immuta_data_object_tag`](docs/resources/data_object_tag.md) | Applies a tag to a registered data object and leaves its other tags alone |
| [`immuta_data_object`](docs/data-sources/data_object.md) (data) | Looks up a registered data object by its path |
| [`immuta_data_source`](docs/data-sources/data_source.md) (data) | Looks up a data source by name, or by connection and path |

**Redshift:** the schema must exist in Redshift before you apply an
`immuta_connection` that uses it. Snowflake and Databricks do not need this
step.

### Data products

A data product is a curated set of data sources that is published in the
Immuta Request app (formerly Marketplace), where people can find it and
request access.

| Resource / data source | What it does |
| --- | --- |
| [`immuta_data_product`](docs/resources/data_product.md) | Creates a data product |
| [`immuta_request_form`](docs/data-sources/request_form.md) (data) | Looks up a request form for a data product |

### Not yet supported

These areas are not supported yet: domain permissions, data product versions
and access requests, connection settings beyond registration and data object
settings (such as crawls), framework versions and cloning, policies on
individual data sources, and projects or purposes. Immuta is replacing
projects and purposes with intents, which are also not supported yet.

## How it talks to Immuta

The provider uses Immuta's published APIs:

- the [V1 API](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api)
  for domains, identity management, tags, identifiers, frameworks and data
  sources
- the [V2 API](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v2-api/policies)
  for global policies
- the [Connections API](https://documentation.immuta.com/saas/developer-guides/api-intro/connections-api)
  for connections and data objects
- the [Request app API](https://documentation.immuta.com/saas/developer-guides/api-intro/marketplace-api)
  for data products

There is one exception. To delete a user, the provider uses the same request
as the Immuta UI, because the published API can only disable users. For the
details, see [`immuta_user`](docs/resources/user.md).

Each resource page in [`docs/`](docs) lists the endpoints it calls.

## Limitations

- **Pre-release.** Features and behaviour may change between versions. Pin
  the exact version you have tested, and try changes on a non-production
  tenant first.
- **Partial coverage.** Only the features listed above can be managed. See
  [Not yet supported](#not-yet-supported).
- **Immuta SaaS only.** The provider has not been tested with self-managed
  Immuta.
- **Not on the Terraform Registry.** You install the provider by hand, as
  described in [Getting started](#getting-started).
- **macOS downloads only.** Release downloads are currently built for Macs
  with Apple silicon. If you need another platform,
  [open an issue](#reporting-issues).

## Reporting issues

Use [GitHub issues](https://github.com/immuta/terraform-provider-immuta/issues)
to report bugs, ask questions and request features. **Do not raise them
with Immuta Support**, because this provider is not part of the Immuta
product and Immuta Support does not cover it.

A useful bug report includes:

- the provider version or commit, the Terraform version and your OS
- the smallest configuration that reproduces the problem
- what you expected to happen and what actually happened
- the relevant output, ideally from `TF_LOG=DEBUG terraform apply`

Remove hostnames, API keys, tokens, user details and any other sensitive
information before you post anything. **Do not report security
vulnerabilities in a public issue.** Use GitHub's
[private vulnerability reporting](https://github.com/immuta/terraform-provider-immuta/security/advisories/new)
instead.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for how to
build, test and submit changes. In short:

1. Open an issue first to discuss any significant change.
2. Fork the repository and create a branch.
3. Add or update tests, docs and examples along with your change.
4. Run `make check` and open a pull request.

## Licence

[Apache 2.0](LICENSE). The licence covers the source code in this
repository only. This provider is not part of the Immuta product, and the
licence grants no rights to Immuta software or services, which remain
governed by your agreement with Immuta. Immuta is a trademark of
Immuta, Inc.
