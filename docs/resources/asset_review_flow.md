---
page_title: "immuta_asset_review_flow Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Overrides the dynamic review flow of an asset for one request type.
---

# immuta_asset_review_flow (Resource)

Overrides the dynamic review flow of an asset for one request type
(`DATA_ACCESS` or `MASKING_EXCEPTION`) with the Request app API:
`PUT /api/asset/{id}/review-flows/{requestType}` saves it,
`GET /api/asset/{id}/review-flows` reads it and
`DELETE /api/asset/{id}/review-flows/{requestType}` removes it.

A review flow decides what happens to an access request: it can collect a
request form, evaluate decision expressions, approve, deny, or route the
request to reviewers. The asset's descendants inherit the flow unless they have
their own. Destroying this resource removes the override, and the asset
inherits the nearest review flow again.

The review flow is a graph document, usually kept in its own file and read with
`file()` or `templatefile()`. Dynamic review flows must be enabled on the
tenant (the `DYNAMIC_REVIEW_FLOWS` feature), and the provider needs the Request
app settings (`request_app_url` and `request_app_token`).

## Example usage

```terraform
# The asset is a data object: here a Custom securable, but any data object
# looked up with data.immuta_data_object works too.
resource "immuta_asset_review_flow" "analyst_access" {
  asset_id     = immuta_custom_data_object.analyst_access.id
  request_type = "DATA_ACCESS"

  review_flow = templatefile("${path.module}/review_flows/analysts.yaml.tftpl", {
    request_form_id = var.analyst_request_form_id
  })
}
```

`review_flows/analysts.yaml.tftpl`:

```yaml
# The root and form node ids are fixed by the Request app API. Every other
# node needs its own UUID, used by the edges.
nodes:
  - id: 53c03cdf-63e1-437c-9f0e-dc68b842da59
    type: root
  - id: a71c93f1-c8b6-4e3c-b219-9bfa22b2d38c
    type: form
    requestFormId: ${request_form_id}
  - id: 6f1c2b9e-0a4d-4c55-9e3b-2d7a8f0c1b11
    type: decision
    expression: '@consumer.groups contains "analysts"'
  - id: 9b2e4c7a-3d5f-4e8a-b1c6-7d8e9f0a1b22
    type: outcome
    outcome: approve
  - id: 0c3f5d8b-4e6a-4f9b-82d7-8e9f0a1b2c33
    type: outcome
    outcome: deny

edges:
  - source: 53c03cdf-63e1-437c-9f0e-dc68b842da59
    target: a71c93f1-c8b6-4e3c-b219-9bfa22b2d38c
  - source: a71c93f1-c8b6-4e3c-b219-9bfa22b2d38c
    target: 6f1c2b9e-0a4d-4c55-9e3b-2d7a8f0c1b11
  # A decision node has a true and a false branch.
  - source: 6f1c2b9e-0a4d-4c55-9e3b-2d7a8f0c1b11
    target: 9b2e4c7a-3d5f-4e8a-b1c6-7d8e9f0a1b22
    branch: true
  - source: 6f1c2b9e-0a4d-4c55-9e3b-2d7a8f0c1b11
    target: 0c3f5d8b-4e6a-4f9b-82d7-8e9f0a1b2c33
    branch: false
```

## Schema

### Required

- `asset_id` (String) ID of the asset, the Immuta data object ID, for example `data.immuta_data_object.<name>.id` or `immuta_custom_data_object.<name>.id`. Changing it replaces the review flow.
- `request_type` (String) The request type the review flow handles: `DATA_ACCESS` or `MASKING_EXCEPTION`. Changing it replaces the review flow.
- `review_flow` (String) The review flow graph, as a YAML or JSON document holding `nodes` and `edges`. See [The review flow document](#the-review-flow-document).

### Read-Only

- `id` (String) `<asset_id>/<request_type>`.

## The review flow document

The document holds only `nodes` and `edges`, the body of the review flow
endpoints.

- Each node has an `id` and a `type`:
  - `root`: exactly one, with the fixed id `53c03cdf-63e1-437c-9f0e-dc68b842da59`.
  - `form`: at most one, with the fixed id `a71c93f1-c8b6-4e3c-b219-9bfa22b2d38c` and a `requestFormId`.
  - `decision`: an `expression` over requestor, consumer, permission, group, attribute or form-answer facts.
  - `outcome`: an `outcome` of `approve`, `deny` or `human-review`; a `human-review` outcome lists its `reviewers`.
- Other nodes need their own UUIDs.
- Each edge has a `source` and a `target` node id. Edges from a decision node have a `branch` of `true` or `false`.

The provider checks this structure when planning. Immuta validates the rest,
such as expressions and reviewers, when the flow is saved, and its reason is
shown if it rejects the flow.

## Behaviour notes

- **Drift**: the flow is compared node by node (by `id`), with edges compared as a set. Formatting, comments, key order, the order of nodes and edges, and fields Immuta adds (such as audit fields and reviewer display names) never cause a diff. A node or edge changed, added or removed in the Request app is planned as an update back to the file.
- **Removed override**: if the override is removed in the Request app, so that the asset inherits its flow again, the next plan saves it again.
- **Create** overwrites any override the asset already has for the request type.

## Import

```shell
# An asset review flow is imported with <asset_id>/<request_type>.
terraform import immuta_asset_review_flow.analyst_access 7d1d0c1e-5b0e-4a77-9a55-3b0f1c2d3e4f/DATA_ACCESS
```

An import records Immuta's graph as JSON. The first plan after the import
shows an update if Immuta's graph has fields the file does not.
