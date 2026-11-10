---
page_title: "immuta_data_object_tag Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Applies one tag to a data object that a connection registered.
---

# immuta_data_object_tag (Resource)

Applies one tag to a data object (a catalog, database, schema or table) that an
[`immuta_connection`](connection.md) registered. It uses the documented
[connections API](https://documentation.immuta.com/saas/developer-guides/api-intro/connections-api):
`GET /api/connections/{connectionKey}/dataObjects/{id}` to read the data
object's tags, and the `tags` field of
`PATCH /api/connections/{connectionKey}/dataObjects/{id}`
(`{"tags": {"add": [{"id": 192}], "remove": []}}`) to apply and remove the tag.

Each resource manages **one tag on one data object**. Other tags on the data
object are never changed, whether a user applied them in the Immuta UI or
Immuta applied them itself (for example, sensitive data discovery). The data object
belongs to the connection: this resource never creates or deletes it.

Find the data object with the [`immuta_data_object`](../data-sources/data_object.md)
data source, and the tag with the [`immuta_tag`](tag.md) resource or the
[`immuta_tag`](../data-sources/tag.md) data source.

## Example usage

```terraform
# Tags managed by Terraform.
resource "immuta_tag" "gold" {
  name = "Access.Layer.Gold"
}

# A tag Terraform does not manage, created in the Immuta UI or by Immuta.
data "immuta_tag" "pii" {
  name = "Discovered.PII"
}

# The data object to tag, found by its path in the connection. The connection
# key is usually immuta_connection.<name>.connection_key.
data "immuta_data_object" "orders" {
  connection_key = "acme_dev_databricks"
  path           = ["acme_dev", "sales", "orders"]
}

# One resource per tag on a data object.
resource "immuta_data_object_tag" "orders_gold" {
  connection_key = data.immuta_data_object.orders.connection_key
  data_object_id = data.immuta_data_object.orders.id
  tag_id         = immuta_tag.gold.id
}

resource "immuta_data_object_tag" "orders_pii" {
  connection_key = data.immuta_data_object.orders.connection_key
  data_object_id = data.immuta_data_object.orders.id
  tag_id         = data.immuta_tag.pii.id
}
```

## Schema

### Required

- `connection_key` (String) Key of the connection that holds the data object,
  usually `data.immuta_data_object.<name>.connection_key`. Changing it replaces
  the resource.
- `data_object_id` (String) ID of the data object to tag, usually
  `data.immuta_data_object.<name>.id`. Changing it replaces the resource.
- `tag_id` (String) Numeric ID of the tag to apply, usually
  `immuta_tag.<name>.id` or `data.immuta_tag.<name>.id`. Changing it replaces
  the resource.

### Read-Only

- `id` (String) `<connection_key>/<data_object_id>/<tag_id>`.
- `tag_name` (String) Fully qualified name of the tag, as Immuta reports it on
  the data object.

## Behaviour notes

- **Only this tag is sent.** Create sends `add` with this tag, and destroy sends
  `remove` with this tag. No settings and no other tags are sent.
- **A tag already applied is not adopted.** If the tag is already applied
  directly to the data object, create fails and tells you how to import it. This
  stops `terraform destroy` from removing a tag that Terraform did not apply.
- **Inherited tags do not count.** A tag applied to a parent (for example the
  schema) shows up on the table as inherited. This resource applies the tag to
  its own data object, even if the tag is already inherited.
- **Drift is detected.** If the tag is removed from the data object outside
  Terraform, it is removed from state and applied again on the next apply.
- **A data object that disappears** (dropped and synced) is removed from state.
- **Policy impact.** Tags drive global policies, so applying or removing a tag
  can change who can see the data object.

## Import

A tag applied to a data object is imported with
`<connection_key>/<data object id>/<tag id>`.

```shell
# A tag applied to a data object is imported with
# <connection_key>/<data object id>/<tag id>.
terraform import immuta_data_object_tag.orders_gold acme_dev_databricks/e6cb340a-4a44-42a5-bed3-0ffe1c491926/192
```
