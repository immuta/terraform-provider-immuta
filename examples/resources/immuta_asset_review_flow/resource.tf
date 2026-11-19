# The asset is a data object: here a Custom securable, but any data object
# looked up with data.immuta_data_object works too.
resource "immuta_custom_data_object" "analyst_access" {
  connection_key = "snowflake-access"
  name           = "Analyst Access"
  type           = "SECURABLE"
}

# The request form the flow collects, by its exact, case-sensitive name.
data "immuta_request_form" "analyst_request" {
  name = "Analyst Access Request"
}

resource "immuta_asset_review_flow" "analyst_access" {
  asset_id     = immuta_custom_data_object.analyst_access.id
  request_type = "DATA_ACCESS"

  review_flow = templatefile("${path.module}/review_flows/analysts.yaml.tftpl", {
    request_form_id = data.immuta_request_form.analyst_request.id
  })
}
