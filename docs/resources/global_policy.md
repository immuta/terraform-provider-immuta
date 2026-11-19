---
page_title: "immuta_global_policy Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages an Immuta global data or subscription policy, written as a YAML or JSON document.
---

# immuta_global_policy (Resource)

Manages an Immuta global policy, either a
[data policy such as a masking policy](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v2-api/policies)
or a [subscription policy](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v2-api/subscription-policy).
The policy is kept as the YAML (or JSON) document you write, in the Immuta V2
policy format, so the examples in the Immuta documentation can be used as they
are.

The Immuta user behind the API key needs the `GOVERNANCE` permission.

## Example usage

Keep one policy per YAML file and manage them all with `for_each`:

```terraform
# Every YAML file in policies/ is one global policy, in the Immuta V2 policy
# format. Adding a file adds a policy and deleting a file deletes it.
resource "immuta_global_policy" "policies" {
  for_each = fileset("${path.module}/policies", "*.yaml")

  policy = file("${path.module}/policies/${each.value}")
}

# A single policy can also be managed on its own.
resource "immuta_global_policy" "hashing" {
  policy = <<-EOT
    name: Hashing (single)
    policyKey: data mask hashing single
    type: data
    actions:
      - rules:
          - type: Masking
            config:
              fields:
                - type: noTags
              maskingConfig:
                type: Hash
    circumstances:
      - type: noTags
  EOT
}
```

`policies/data-mask-hashing.yaml`, the documented simple masking policy:

```yaml
name: Hashing
policyKey: data mask hashing
type: data
actions:
  - rules:
      - type: Masking
        config:
          fields:
            - type: noTags
          maskingConfig:
            type: Hash
circumstances:
  - type: noTags
```

`policies/subscription-entitlements.yaml`, the documented subscription policy:

```yaml
name: Entitlement
policyKey: subscription entitlements
type: subscription
actions:
  type: entitlements
  entitlements:
    operator: any
    groups:
      - Employee
    attributes:
      - name: auth1
        value: SOMETHING_ELSE
  automaticSubscription: true
  allowDiscovery: false
  description: Some description here
circumstances:
  - type: columnRegex
    regex: ssn
    caseInsensitive: false
staged: false
```

The [examples directory](https://github.com/immuta/terraform-provider-immuta/tree/main/examples/resources/immuta_global_policy/policies)
also holds the documented constant masking, advanced entitlement and manual
subscription policies.

Use `staged: true` to save a policy in Immuta without enforcing it.

## Schema

### Required

- `policy` (String) The policy, as a YAML or JSON document in the Immuta V2
  policy format, usually read with `file()`. It needs `policyKey`, `name`,
  `type` (`data` or `subscription`) and `actions`.

### Read-Only

- `id` (String) ID of the global policy, assigned by Immuta.
- `policy_key` (String) The `policyKey` that identifies the policy in Immuta.
- `name` (String) Name of the policy.
- `type` (String) Type of the policy: `data` or `subscription`.

## Behaviour notes

- **Formatting is not a change.** Comments, indentation, key order, flow
  style, and YAML versus JSON never produce a diff. Changing a value, or the
  order of a list, does.
- **Updates are in place.** A changed document is saved again with
  `POST /api/v2/policy`, which Immuta matches to the existing policy by its
  `policyKey`.
- **A new `policyKey` or `type` replaces the policy.** Saving a document
  with another key would otherwise create a second policy and leave the first
  one behind.
- **Validation at plan time.** A document that is not valid YAML, is not a
  mapping, holds more than one YAML document, misses `policyKey`, `name` or
  `actions`, or has another `type` fails the plan. A top-level key that is not
  documented, such as a misspelt `circumstanceOperator`, produces a warning.
  The rest of the document is checked by Immuta when it is saved.
- **Drift detection is indirect.** The V2 API cannot read a policy back, so
  the provider reads it with the V1 global policy API. A policy deleted, or
  renamed, in Immuta is detected and restored on the next apply. Other
  changes made in Immuta, such as to the rules, are detected by comparing the
  V1 content with the content recorded when the document was last saved; the
  plan then shows `policy` changing and the apply saves the document again.
  The plan cannot show what was changed in Immuta.
- **One update after an upgrade or import.** When no content is recorded for
  a policy, as after an import or an upgrade from a provider version that did
  not record it, the next plan saves the document again once, so that a
  change already made in Immuta is not taken as the baseline.
- **File names are resource addresses.** With `for_each` over `fileset()`,
  renaming a file destroys and recreates its policy. Use a `moved` block to
  rename a file without doing so.

## Destructive behaviour

Destroying an `immuta_global_policy` deletes the policy in Immuta, which stops
enforcing it on every data source it applies to. Replacing a policy deletes
the old one before the new one is created, so it is briefly not enforced.

## Import

Global policies are imported with their ID. The policy document cannot be
read back in the V2 format, so the first apply after an import saves the
configured document. If its `policyKey` differs from the imported policy's,
the policy is replaced.

```shell
terraform import 'immuta_global_policy.policies["data-mask-hashing.yaml"]' 42
```
