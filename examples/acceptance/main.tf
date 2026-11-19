# A complete configuration used to verify the provider by hand against a
# sandbox tenant. See README.md in this directory.

terraform {
  required_version = ">= 1.0"

  required_providers {
    immuta = {
      source = "immuta/immuta"
    }
  }
}

variable "domain_name" {
  description = "Name of the domain to create. Use a prefix that identifies it as test data."
  type        = string
  default     = "tf-acc-immuta-example"
}

# host comes from IMMUTA_HOST and api_key from IMMUTA_API_KEY.
provider "immuta" {}

resource "immuta_domain" "example" {
  name        = var.domain_name
  description = "Created by the terraform-provider-immuta example configuration"
}

output "domain" {
  value = {
    id              = immuta_domain.example.id
    name            = immuta_domain.example.name
    type            = immuta_domain.example.type
    assignment_type = immuta_domain.example.assignment_type
    created_at      = immuta_domain.example.created_at
  }
}
