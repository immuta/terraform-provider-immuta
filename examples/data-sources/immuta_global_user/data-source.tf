# Look up a user's Immuta global user ID by username. Needs the provider's
# request_app_url and request_app_token.
data "immuta_global_user" "jane" {
  username = "jane.doe@immuta.com"
}

# iam_id is needed when the same username exists in several identity
# managers.
data "immuta_global_user" "sam" {
  username = "sam.smith"
  iam_id   = "bim"
}

resource "immuta_data_product" "customer_360" {
  name                     = "Customer 360"
  domains                  = ["0b6c1d1e-6a1f-4c0e-9d0c-1f2e3a4b5c6d"]
  subject_matter_expert_id = data.immuta_global_user.jane.global_user_id
  data_sources             = ["12"]
}
