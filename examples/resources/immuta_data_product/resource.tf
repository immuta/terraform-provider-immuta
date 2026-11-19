resource "immuta_domain" "sales" {
  name = "Sales"
}

# Data sources are registered and assigned to the domain outside this
# configuration; the data source lists them.
data "immuta_domain_data_sources" "sales" {
  domain_id = immuta_domain.sales.id
}

# The subject matter expert, by username.
data "immuta_global_user" "expert" {
  username = "jane.doe@immuta.com"
}

# Immuta requires request forms to create a data product.
data "immuta_request_form" "standard" {
  name = "Standard Request Form"
}

resource "immuta_data_product" "customer_360" {
  name        = "Customer 360"
  description = "Everything about customers"
  domains     = [immuta_domain.sales.id]

  # The subject matter expert's Immuta global user ID.
  subject_matter_expert_id = data.immuta_global_user.expert.global_user_id

  data_sources = data.immuta_domain_data_sources.sales.ids

  # The same form can be used for both.
  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.standard.id
}

# Choosing the id keeps the data product the same in every environment.
resource "immuta_data_product" "orders" {
  id                       = "4c8f6d82-e4c6-4478-92eb-d9342500f006"
  name                     = "Orders"
  domains                  = [immuta_domain.sales.id]
  subject_matter_expert_id = "99aeadb5-352e-4872-9983-e628d399c36e"
  data_sources = [
    for data_source in data.immuta_domain_data_sources.sales.data_sources :
    data_source.id if startswith(data_source.name, "Orders")
  ]

  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.standard.id
}
