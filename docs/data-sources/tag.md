---
page_title: "immuta_tag Data Source - terraform-provider-immuta"
subcategory: ""
description: |-
  Looks up an existing Immuta tag by name.
---

# immuta_tag (Data Source)

Looks up an existing Immuta tag by its fully qualified name, with the documented
`GET /tag` search. Use it to reference tags Terraform does not manage, such as
tags under `Discovered` or tags created in the Immuta UI, for example in
[`immuta_data_object_tag`](../resources/data_object_tag.md). For a tag managed
with the [`immuta_tag`](../resources/tag.md) resource, reference that resource
instead.

## Example usage

```terraform
# Look a tag up by its fully qualified name, with dots between parent and
# child tags.
data "immuta_tag" "gold" {
  name = "Access.Layer.Gold"
}

output "gold_tag_id" {
  value = data.immuta_tag.gold.id
}
```

## Schema

### Required

- `name` (String) Fully qualified name of the tag, with a dot separating a
  parent tag from its child, for example `Access.Layer.Gold`. The match is
  exact and case-sensitive.

### Read-Only

- `id` (String) Numeric ID of the tag, the same identifier as `immuta_tag.id`.
- `source` (String) Where the tag came from, for example `curated`.
- `system_created` (Boolean) Whether Immuta created the tag itself rather than
  a user creating it.

The lookup fails with an error when no tag has the name.
