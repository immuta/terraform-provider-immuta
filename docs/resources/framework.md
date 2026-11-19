---
page_title: "immuta_framework Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages an Immuta classification framework with the documented /frameworks API.
---

# immuta_framework (Resource)

Manages an [Immuta classification framework](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/frameworks).

A framework applies classification tags to columns, based on the tags already
found on the same column, on its neighbouring columns or on its data source.
It can also assign a sensitivity to its tags, which drives the audit dashboards
and monitors.

The Immuta user behind the API key needs the `GOVERNANCE` permission.

## Example usage

```terraform
resource "immuta_tag" "internal_employee_data" {
  name = "HR Framework.Internal Employee Data"
}

resource "immuta_tag" "employee_name" {
  name = "Discovered.Employee Name"
}

resource "immuta_framework" "hr" {
  short_name  = "HR Information"
  name        = "HR Information Framework"
  description = "Finds internal employee information and tags it for HR."
  active      = true

  tags = [
    {
      name   = immuta_tag.internal_employee_data.name
      source = "curated"
      sensitivities = [
        { dimension = "confidentiality", sensitivity = 1 },
      ]
    },
  ]

  rules = [
    {
      name               = "Employee data"
      classification_tag = { name = immuta_tag.internal_employee_data.name, source = "curated" }
      neighbor_column_tags = [
        { name = immuta_tag.employee_name.name, source = "curated" },
      ]
    },
  ]
}
```

## Loading rules from YAML

Rules can live in a YAML file and be loaded with the
[`decode_framework_rules`](../functions/decode_framework_rules.md) function
(Terraform 1.8 or later):

```terraform
rules = provider::immuta::decode_framework_rules(file("${path.module}/rules.yaml"))
```

Prefer it over `yamldecode`, which silently drops misspelt keys and so can
create a rule without one of its criteria.

## Schema

### Required

- `short_name` (String) Short, human-readable name of the framework.
- `name` (String) Official, human-readable name of the framework.
- `description` (String) Description of the framework. Immuta requires one.
- `active` (Boolean) Whether Immuta actively uses the framework on data
  sources.
- `tags` (Attributes Set) Tags used in the framework, and the sensitivity
  attached to them. Each tag the framework uses must have an entry. See
  [below for nested schema](#nestedatt--tags).
- `rules` (Attributes Set) Rules that apply the framework's tags. At most 50.
  See [below for nested schema](#nestedatt--rules).

### Read-Only

- `id` (String) Unique ID of the framework, assigned by Immuta.
- `version` (String) ID of the current version of the framework. It changes
  every time the framework is updated.
- `created_by` (Number) ID of the user who created the framework. Null when
  Immuta does not return a numeric user ID.
- `created_at` (String) Timestamp of when the framework was created.

<a id="nestedatt--tags"></a>
### Nested schema for `tags`

Required:

- `name` (String) Fully qualified name of the tag, including any parent tags.
- `source` (String) Catalog the tag is from. `curated` is any tag in Immuta;
  otherwise use the external catalog ID.

Optional:

- `sensitivities` (Attributes Set) Sensitivity assigned to the tag. Omit for
  no sensitivity; an empty set is rejected.
  - `dimension` (String, Required) Type of sensitivity. The only documented
    value is `confidentiality`.
  - `sensitivity` (Number, Required) `0` for non-sensitive, `1` for sensitive
    and `2` for highly-sensitive data.

<a id="nestedatt--rules"></a>
### Nested schema for `rules`

Required:

- `name` (String) Short, human-readable name of the rule.
- `classification_tag` (Attributes) Tag to apply when the criteria match,
  with a required `name` and `source` as for `tags`.

Optional criteria. Each is a set of tags with a required `name` and `source`.
Omit a criterion that is not used; an empty set is rejected.

- `column_tags` (Attributes Set) Apply the classification tag to a column when
  one of these tags is found on the same column.
- `neighbor_column_tags` (Attributes Set) Apply the classification tag to
  every column in a data source when one of these tags is found on any of its
  columns.
- `table_tags` (Attributes Set) Apply the classification tag to every column
  in a data source when one of these tags is found on the data source.

## Behaviour notes

- `tags`, `rules` and every list inside them are sets, so the order Immuta
  returns them in never produces a diff.
- An unset optional set is sent to Immuta as an empty list, so removing a
  criterion or a sensitivity from the configuration removes it in Immuta.
- Every update sends the complete framework with
  `PUT /frameworks/{frameworkId}`, so changes made outside Terraform are
  overwritten on the next apply. Immuta assigns a new `version` on each update.
- Reference the tags with `immuta_tag` where you manage them in Terraform, so
  they are created before the framework that uses them and destroyed after
  it. Immuta refuses to delete a tag while a framework uses it.
- System-created frameworks cannot be edited. Import and manage only
  frameworks you created, or a clone of a system framework made in Immuta.
- Framework versions and cloning are not managed by this resource.

## Destructive behaviour

Destroying an `immuta_framework` deletes it in Immuta, and the framework stops
classifying data. Tags it has already applied to columns are not covered by
the documented API, so check them in Immuta after removing a framework.

## Import

Frameworks are imported with the ID Immuta assigned them.

```shell
terraform import immuta_framework.hr 9a6bf3b1-823c-4b2e-aef9-570dac6793cc
```
