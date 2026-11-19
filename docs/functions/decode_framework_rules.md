---
page_title: "decode_framework_rules Function - terraform-provider-immuta"
subcategory: ""
description: |-
  Strictly decodes framework rules from YAML.
---

# decode_framework_rules (Function)

Strictly decodes a YAML (or JSON) list of framework rules into the value
expected by the `rules` attribute of [`immuta_framework`](../resources/framework.md).

Provider-defined functions need **Terraform 1.8 or later**. The rest of the
provider does not.

## Why not `yamldecode`?

`rules = yamldecode(file("rules.yaml"))` works, but Terraform silently drops
any key the schema does not know. A misspelt criterion such as
`neighbour_column_tags`, or the Immuta API spelling `columnTags`, produces a
rule **without** that criterion and no error. `decode_framework_rules` rejects
unknown keys instead, and reports the line they are on.

## Example usage

```terraform
resource "immuta_framework" "hr" {
  # ...
  rules = provider::immuta::decode_framework_rules(file("${path.module}/rules.yaml"))
}
```

`rules.yaml`:

```yaml
- name: Employee data
  classification_tag: { name: HR Framework.Internal Employee Data, source: curated }
  neighbor_column_tags:
    - { name: Discovered.Employee Name, source: curated }

- name: Salary
  classification_tag:
    name: HR Framework.Salary
    source: curated
  column_tags:
    - name: Discovered.Salary
      source: curated
  table_tags:
    - { name: Payroll, source: curated }
```

## Signature

```text
decode_framework_rules(document string) list of object
```

## Arguments

1. `document` (String) YAML document holding a list of rules, typically read
   with `file()`.

## Document format

Each rule accepts the following keys. Each key can be spelt as in Terraform or
as in the Immuta API, but a rule cannot use both spellings of the same key.

| Terraform              | Immuta API           | Required | Value                      |
|------------------------|----------------------|----------|----------------------------|
| `name`                 | `name`               | Yes      | Text                       |
| `classification_tag`   | `classificationTag`  | Yes      | A tag                      |
| `column_tags`          | `columnTags`         | No       | A list of tags             |
| `neighbor_column_tags` | `neighborColumnTags` | No       | A list of tags             |
| `table_tags`           | `tableTags`          | No       | A list of tags             |

A tag is a mapping with a required `name` and `source` (`curated` for tags in
Immuta). An empty criteria list is the same as leaving it out.

The function fails, and so does the plan, when the document:

- has a key that is not listed above, at any level;
- is missing a rule name, a classification tag, or a tag name or source;
- is not a list, is empty, or holds more than one YAML document.

Rule limits, such as the maximum of 50 rules, are still checked by
`immuta_framework` itself.

The tags the rules use still need entries in the framework's `tags`.
