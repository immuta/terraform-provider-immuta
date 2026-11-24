---
page_title: "immuta_identifier Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages an Immuta identifier with the documented /sdd/identifier API.
---

# immuta_identifier (Resource)

Manages an [Immuta identifier](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/manage-identifiers-in-domains).

An identifier tells sensitive data discovery what to look for and which tags
to apply when it finds it.

Set `domain_id` to create the identifier inside a domain, or leave it unset to
create a reference identifier available across the tenant. To put an existing
reference identifier to work inside a domain, use
[`immuta_domain_identifier`](domain_identifier.md) instead.

## Example usage

```terraform
resource "immuta_tag" "primary_color" {
  name = "Discovered.Primary color"
}

resource "immuta_identifier" "primary_color" {
  name        = "Primary colors"
  description = "Columns that hold a primary color"
  domain_id   = immuta_domain.finance.id
  type        = "regex"
  regex       = "(red|green|blue)"
  tags        = [immuta_tag.primary_color.name]
}
```

## Schema

### Required

- `name` (String) Name of the identifier.
- `description` (String) Description of the identifier. Immuta requires one.
- `type` (String) Criteria type: `regex` matches column values,
  `columnNameRegex` matches column names, `dictionary` matches column values
  against a list.
- `tags` (Set of String) Fully qualified names of the tags to apply when the
  criteria matches. Each tag must already exist; use `immuta_tag`.

### Optional

- `domain_id` (String) ID of the domain to create the identifier in. Omit for
  a tenant-wide reference identifier. Changing it replaces the identifier.
- `regex` (String) Case-insensitive regular expression. Required when `type`
  is `regex` or `columnNameRegex`, and rejected otherwise.
- `values` (Set of String) Values the dictionary matches. Required when `type`
  is `dictionary`, and rejected otherwise.
- `case_sensitive` (Boolean) Whether dictionary values match
  case-sensitively. Only applies to `dictionary`. Defaults to `false`.

### Read-Only

- `id` (String) Unique ID of the identifier, assigned by Immuta.
- `created_at` (String) Timestamp of when the identifier was created.
- `updated_at` (String) Timestamp of when the identifier was last updated.

## Behaviour notes

- **`type` and its configuration are validated at plan time.** A `regex`
  identifier with `values`, or a `dictionary` identifier with `regex`, fails
  during `terraform plan` rather than part way through an apply.
- **Updates replace the whole identifier.** Immuta does not support partial
  updates of an identifier, so every apply sends the complete definition.
  Changing `name`, `description`, `type`, `regex`, `values` or `tags` is
  applied in place; only `domain_id` forces replacement.
- **Tags must already exist.** Immuta rejects an identifier that references a
  tag it does not know about. The provider turns that rejection into a
  diagnostic that points at `immuta_tag`.
- **Remote changes show up as drift.** An identifier edited or deleted in the
  Immuta UI produces a diff on the next plan.

## Destructive behaviour

Destroying an `immuta_identifier` deletes it in Immuta. Sensitive data
discovery stops applying its tags to newly scanned columns.

Tags already applied to existing columns are not removed by deleting the
identifier, so the two can drift apart. Check what depends on the tags before
removing the identifier that applies them.

## Import

Identifiers are imported with the ID Immuta assigned them.

```shell
terraform import immuta_identifier.primary_color a32c69df-3707-4b03-8144-a8d11d1f52a4
```
