resource "immuta_group" "engineering" {
  name        = "Engineering"
  description = "Engineering department"
  email       = "engineering@immuta.com"
}

# Server managed attributes are available for downstream use.
output "engineering_group_id" {
  value = immuta_group.engineering.id
}
