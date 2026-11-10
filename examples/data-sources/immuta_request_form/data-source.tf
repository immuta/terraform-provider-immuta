# A Request app request form, by its exact, case-sensitive name.
data "immuta_request_form" "standard" {
  name = "Standard Request Form"
}

resource "immuta_data_product" "customer_360" {
  name                     = "Customer 360"
  domains                  = ["0b6a7c1e-5d1f-4d43-9a8e-1f2b3c4d5e6f"]
  subject_matter_expert_id = "99aeadb5-352e-4872-9983-e628d399c36e"
  data_sources             = ["42"]

  # The same form can be used for both.
  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.standard.id
}
