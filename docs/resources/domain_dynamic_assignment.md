---
page_title: "immuta_domain_dynamic_assignment Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Assigns data sources to a domain dynamically by tag.
---

# immuta_domain_dynamic_assignment (Resource)

Assigns data sources to an [`immuta_domain`](domain.md) dynamically by tag,
with the documented
[`POST /domain/{domainId}/assignment`](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/domains-api):
every data source with any of the tags is in the domain, including data
sources tagged later. This is the way Immuta recommends to keep a domain's data
sources up to date, for example for
[`immuta_data_product`](data_product.md).

To add specific data sources instead, use
[`immuta_domain_data_source_assignment`](domain_data_source_assignment.md).

## Example usage

```terraform
resource "immuta_domain" "denmark" {
  name = "Denmark"
}

# Every data source tagged with any of these tags is in the domain, now and
# as data sources are tagged later.
resource "immuta_domain_dynamic_assignment" "denmark" {
  domain_id = immuta_domain.denmark.id
  tags = [
    "Discovered.Country.Denmark",
  ]
}
```

## Schema

### Required

- `domain_id` (String) ID of the domain, usually `immuta_domain.<name>.id`.
  Changing it replaces the assignment.
- `tags` (Set of String) Names of the tags whose data sources are in the
  domain, for example `Discovered.Country.Denmark` or
  `immuta_tag.<name>.name`. At least one is required.

### Read-Only

- `id` (String) ID of the domain, as `domain_id`.

## Behaviour notes

- **One per domain.** The assignment type belongs to the domain: declare at
  most one of these resources for each domain.
- **Changing `tags` updates in place.** Immuta adds the data sources with the
  new tags and removes the ones that no longer match; the provider logs how
  many at `INFO` level.
- **Apply waits for Immuta.** Immuta applies the assignment in the
  background. Create and update wait, for up to 5 minutes, until the domain
  shows the tags and lists as many data sources as Immuta reported, so that
  [`immuta_domain_data_sources`](../data-sources/domain_data_sources.md) reads
  them in the same apply.
- **Destroy sets the domain back to manual assignment.** The data sources the
  assignment added stay in the domain, and Immuta then refuses to delete the
  domain. To destroy the domain in the same apply, set
  [`force_destroy = true`](domain.md) on it and apply that first.
- **Drift is detected:** a domain switched to manual, or given other tags, in
  Immuta shows as a change and is put back on the next apply. A deleted domain
  removes the assignment from state.
- **`immuta_domain.assignment_type`** reads `dynamic` after the next refresh;
  it is read-only there, so the two resources never conflict.

## Import

The assignment is imported with the domain ID.

```shell
# The assignment is imported with the domain ID.
terraform import immuta_domain_dynamic_assignment.denmark 9d131f6d-7d66-43c8-81f5-dda82f6e9270
```
