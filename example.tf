terraform {
  required_providers {
    immuta = {
      source = "immuta/immuta"
    }
  }
}

provider "immuta" {
  api_token = "alphanumeric" # DO NOT HARDCODE SECRETS
  host = "my-immuta.hosted.immutacloud.com" # FQDN
}

# wrapper for
# https://documentation.immuta.com/saas/developer-guides/api-intro/immuta-v1-api/configure-your-instance-of-immuta/bim#update-a-users-or-groups-attributes
resource "immuta_bim_attribute" "my_immuta_tenant" {
  iam_id = "bim"
  model_type = "user" # group or user
  model_id = "29" # group or user id
  key = "Security Classification"
  value = "Proprietary"
}
