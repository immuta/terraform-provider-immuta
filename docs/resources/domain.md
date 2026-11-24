---
page_title: "immuta_domain Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Manages an Immuta domain with the documented /domain API.
---

# immuta_domain (Resource)

Manages an [Immuta domain](https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/domains-api).

A domain is an organisational boundary that groups data sources and delegates
policy authoring. Creating a domain does not register data sources, enable
Policy Sync, or author subscription or data policies.

Managing a domain requires the `GOVERNANCE` permission.

## Example usage

```terraform
resource "immuta_domain" "research" {
  name         = "Research"
  description  = "Domain for the research organisation"
  autoscanning = true
}

output "research_domain_id" {
  value = immuta_domain.research.id
}
```

## Schema

### Required

- `name` (String) Name of the domain. Changing the name updates the existing
  domain in place rather than replacing it.

### Optional

- `description` (String) Description of the domain that is displayed to users.
  Removing the argument clears the description in Immuta.
- `type` (String) Type of the domain. The only documented value is `domain`,
  which is also the default. Changing this value replaces the domain.
- `autoscanning` (Boolean) Whether identification autoscanning is enabled for
  the domain. When it is on, identification runs automatically for every data
  source added to the domain. Set with the documented
  `PUT /domain/{domainId}/settings`. When omitted, the provider leaves the
  setting as it is in Immuta and reports its value.
- `force_destroy` (Boolean) When `true`, destroying the domain first removes
  every data source from it, then deletes the domain. Defaults to `false`.
  See [Destructive behaviour](#destructive-behaviour). Not sent to Immuta, and
  not set on import.

### Read-Only

- `id` (String) Unique identifier of the domain, assigned by Immuta.
- `assignment_type` (String) How data sources are assigned to the domain,
  `manual` or `dynamic`.
- `created_by` (Number) Profile ID of the user that created the domain.
- `created_at` (String) Timestamp of when the domain was created.
- `updated_at` (String) Timestamp of when the domain was last updated.

`assignment_type` is managed through a separate documented endpoint, not by
this resource. It is exposed read-only so that configuration can refer to it.
To assign data sources by tag, use
[`immuta_domain_dynamic_assignment`](domain_dynamic_assignment.md).

## Behaviour notes

- **Autoscanning is only sent when configured.** On create, and on update
  when the value changes, the provider sends
  `PUT /domain/{domainId}/settings` after the domain itself. A change made in
  the Immuta UI shows up as drift and is put back on the next apply. Removing
  `autoscanning` from the configuration does not change the setting in Immuta.
  If the setting fails to apply on create, the domain is still saved to state
  and marked tainted, so that the next apply replaces it.
- **Name changes update in place.** The documented `PUT /domain/{domainId}`
  endpoint changes the name and description, so renaming a domain does not
  destroy it or its contents.
- **Deleting a non-empty domain fails** unless `force_destroy` is set. Immuta
  rejects the delete when the domain still contains data sources, policies,
  data products or identifiers. The provider surfaces that conflict, with what
  `DELETE /domain/{domainId}?dryRun=true` counts, rather than hiding it.
- **Remote changes show up as drift.** A description edited in the Immuta UI
  produces a diff on the next plan. A domain deleted outside Terraform is
  removed from state and planned for recreation.
- **An unset description stays null.** Immuta returns an empty description for
  a domain that has none; the provider keeps that as `null` instead of
  producing a perpetual diff.

## Destructive behaviour

Destroying an `immuta_domain` deletes the domain in Immuta. Immuta refuses the
delete while the domain still contains data sources, policies, data products
or identifiers, so by default a destroy fails rather than silently removing
governed objects. Detach or delete the contents first.

Data sources added by an
[`immuta_domain_dynamic_assignment`](domain_dynamic_assignment.md) stay in the
domain when the assignment is destroyed, so destroying both in one apply fails
without `force_destroy`.

### `force_destroy`

```terraform
resource "immuta_domain" "research" {
  name          = "Research"
  force_destroy = true
}
```

With `force_destroy = true`, destroying the domain:

1. lists its data sources with `GET /domain/{domainId}/datasources`,
2. removes each one from the domain with
   `DELETE /collection/{domainId}/datasources/{dataSourceId}`, the endpoint the
   Immuta UI uses (it is not in the published Domains API reference),
3. deletes the domain.

- It removes **every** data source in the domain, however it was added,
  including any managed by `immuta_domain_data_source_assignment`. The data
  sources themselves are not deleted.
- It does not remove global policies, identifiers or data products: Immuta
  still refuses the delete while any are left.
- A data source that is already gone from the domain is skipped.
- If removing a data source fails, the provider stops, does not delete the
  domain, and reports which data sources it already removed. Applying again
  continues with the rest.
- Terraform deletes with the attribute values in state, so set
  `force_destroy = true` and apply it before the destroy that relies on it.
  Changing it only updates the state.

Deleting a domain also removes the domain-scoped permissions and the policy
authoring delegation that depend on it. Plan domain removal in a sandbox
tenant before doing it anywhere that matters.

## Testing this resource

Verify behaviour against a sandbox tenant before relying on it:

```sh
export IMMUTA_HOST="https://sandbox.immuta.com"
export IMMUTA_API_KEY="..."
export IMMUTA_TEST_DOMAIN_NAME_PREFIX="tf-acc-$(whoami)"

# Terraform independent check of connectivity, credentials and permissions.
make smoke-domain

# Full create, no-op plan, update, import and destroy cycle, with every step
# verified against a direct API read.
make testacc
```

Both commands create throwaway domains and clean up after themselves. Never
point them at a production tenant.

## Import

Domains are imported with the domain id assigned by Immuta.

```shell
terraform import immuta_domain.research c7458ee9-03e5-4b6f-8395-000000000001
```

The id is the value returned by `GET /domain`, or the last path segment of the
domain URL in the Immuta UI.
