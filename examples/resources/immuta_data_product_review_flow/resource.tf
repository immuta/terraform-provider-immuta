resource "immuta_data_product_review_flow" "sales_data_access" {
  data_product_id = immuta_data_product.sales.id
  request_type    = "DATA_ACCESS"
  review_flow     = file("${path.module}/review_flows/auto_approve.yaml")
}
