---
page_title: "immuta_data_product_review_flow Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Sets the dynamic review flow of a data product for one request type.
---

# immuta_data_product_review_flow (Resource)

Sets the dynamic review flow of a data product for one request type
(`DATA_ACCESS` or `MASKING_EXCEPTION`) with the Request app API:
`PUT /api/data-product/{id}/review-flows/{requestType}` saves it, and
`GET /api/data-product/{id}` reads it back.

It needs the GOVERNANCE permission or Manage Data Products in the data
product's domain. Dynamic review flows must be enabled on the tenant (the
`DYNAMIC_REVIEW_FLOWS` feature), and the provider needs the Request app
settings (`request_app_url` and `request_app_token`).

**A data product always has a review flow, and the API cannot remove one.
Destroying this resource only removes it from the Terraform state: the last
saved flow stays on the data product, and Terraform shows a warning.**

## Example usage

```terraform
resource "immuta_data_product_review_flow" "sales_data_access" {
  data_product_id = immuta_data_product.sales.id
  request_type    = "DATA_ACCESS"
  review_flow     = file("${path.module}/review_flows/auto_approve.yaml")
}
```

`review_flows/auto_approve.yaml`:

```yaml
# Approve every request straight away.
nodes:
  - id: 53c03cdf-63e1-437c-9f0e-dc68b842da59
    type: root
  - id: 4d6e8f0a-1b2c-4d3e-8f5a-6b7c8d9e0f44
    type: outcome
    outcome: approve

edges:
  - source: 53c03cdf-63e1-437c-9f0e-dc68b842da59
    target: 4d6e8f0a-1b2c-4d3e-8f5a-6b7c8d9e0f44
```

## Schema

### Required

- `data_product_id` (String) ID of the data product, usually `immuta_data_product.<name>.id`. Changing it replaces the review flow.
- `request_type` (String) The request type the review flow handles: `DATA_ACCESS` or `MASKING_EXCEPTION`. Changing it replaces the review flow.
- `review_flow` (String) The review flow graph, as a YAML or JSON document holding `nodes` and `edges`. The document is the same as for [`immuta_asset_review_flow`](asset_review_flow.md#the-review-flow-document).

### Read-Only

- `id` (String) `<data_product_id>/<request_type>`.

## Behaviour notes

- **Drift** is detected as for [`immuta_asset_review_flow`](asset_review_flow.md#behaviour-notes).
- **Legacy review flows**: `GET /api/data-product/{id}` only returns a flow that is a review flow graph. If the data product has none, for example a legacy flow, the next plan saves the flow again.
- **`immuta_data_product`** does not manage review flows. With dynamic review flows, set the request form in the flow's `form` node.

## Import

```shell
# A data product review flow is imported with <data_product_id>/<request_type>.
terraform import immuta_data_product_review_flow.sales_data_access 5e1c0d2b-0f7a-4b8e-9c3d-2a1b0c9d8e7f/DATA_ACCESS
```
