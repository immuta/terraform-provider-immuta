terraform {
  required_providers {
    immuta = {
      source = "immuta/immuta"
    }
  }
}

provider "immuta" {
  api_token =""
  host =""
}

resource "immuta_bim_attribute" "my_immuta" {
}
