data "immuta_request_form" "standard" {
  name = "Standard Request Form"
}

resource "immuta_data_product" "sales" {
  name                     = "Sales"
  domains                  = ["0b6a7c1e-5d1f-4d43-9a8e-1f2b3c4d5e6f"]
  subject_matter_expert_id = "99aeadb5-352e-4872-9983-e628d399c36e"
  data_sources             = ["42"]

  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.standard.id
}

resource "immuta_data_product_review_flow" "sales_data_access" {
  data_product_id = immuta_data_product.sales.id
  request_type    = "DATA_ACCESS"
  review_flow     = file("${path.module}/review_flows/auto_approve.yaml")
}
