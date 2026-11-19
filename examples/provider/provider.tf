terraform {
  required_version = ">= 1.0"

  required_providers {
    immuta = {
      source = "immuta/immuta"
    }
  }
}

# The API key is deliberately not set here. Export it instead:
#
#   export IMMUTA_API_KEY="..."
#
# so that the credential never ends up in configuration or in version control.
#
# immuta_data_product also needs the regional Request app API and a personal
# access token, which is separate from the API key:
#
#   export IMMUTA_REQUEST_APP_TOKEN="..."
provider "immuta" {
  host            = "https://example.immuta.com"
  request_app_url = "https://na.api.immutacloud.com/marketplace"
}
