---
page_title: "immuta_tag Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages an Immuta tag with the documented /tag API.
---

# immuta_tag (Resource)

Manages an [Immuta tag](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/tagging).

Tags are what identifiers apply to the columns they match, and what policies
match on. An identifier can only reference a tag that already exists, so this
resource is normally paired with [`immuta_identifier`](identifier.md).

## Example usage

```terraform
resource "immuta_tag" "primary_color" {
  name = "Discovered.Primary color"
}

resource "immuta_identifier" "primary_color" {
  name        = "Primary colors"
  description = "Columns that hold a primary color"
  type        = "regex"
  regex       = "(red|green|blue)"
  tags        = [immuta_tag.primary_color.name]
}
```

Reference `immuta_tag.<name>.name` rather than typing the string. That gives
Terraform the dependency it needs to create the tag before the identifier
that uses it, which is the whole reason these are two resources.

## Schema

### Required

- `name` (String) Fully qualified name of the tag, with a dot separating a
  parent tag from its child, for example `Discovered.Primary color`. Changing
  the name replaces the tag.

### Optional

- `delete_hierarchy` (Boolean) Whether destroying this resource also deletes
  every child tag beneath it. Defaults to `false`. Changing it sends no
  request; it only affects destroy.

### Read-Only

- `id` (String) Numeric ID of the tag, assigned by Immuta.
- `source` (String) Where the tag came from. Tags created through the API are
  `curated`.
- `system_created` (Boolean) Whether Immuta created the tag itself.

## Behaviour notes

- **Parent tags are created implicitly.** Creating `Example.Primary color`
  also creates `Example` if it does not exist. Immuta owns those parents,
  and Terraform does not track or delete them.
- **Children are added under an existing root.** Immuta rejects a plain
  create under a root tag that already exists, which is the usual case for
  `Discovered`, the hierarchy Immuta creates for sensitive data discovery.
  The provider checks whether the root exists and, if it does, adds the tag
  with the documented `rootTag` field, exactly as the Immuta UI does. It
  always sends `rootTag.deleteHierarchy = false`, so the root's existing
  children are never touched.
- **Tag creation is serialised.** Whether a create needs `rootTag` depends on
  whether the root exists yet, so the provider creates tags one at a time.
  Several tags under a brand new root in one apply therefore all succeed.
- **Tags that already exist must be imported.** Creating a tag that is
  already in Immuta fails; import it instead.
- **Deleting a tag with children fails** unless `delete_hierarchy = true`.
- **Remote changes show up as drift.** A tag deleted in the Immuta UI is
  removed from state and planned for recreation.

## Destructive behaviour

Destroying an `immuta_tag` deletes the tag in Immuta. Any policy that matches
on it stops applying, and any identifier that applies it breaks.

With `delete_hierarchy = true` the blast radius is much wider, because every
child tag goes too. Check what references the tag first, in a sandbox tenant.

## Import

Tags are imported with their fully qualified name.

```shell
terraform import immuta_tag.primary_color 'Discovered.Primary color'
```
