resource "immuta_user" "jane" {
  user_id     = "jane.doe@immuta.com"
  name        = "Jane Doe"
  email       = "jane.doe@immuta.com"
  permissions = ["CREATE_PROJECT"]
}

# Suspend access without deleting the account. Destroying the resource instead
# deletes the user permanently and cannot be undone.
resource "immuta_user" "contractor" {
  user_id  = "sam.smith@contractor.example"
  name     = "Sam Smith"
  disabled = true
}

# Server managed attributes are available for downstream use. The numeric id
# is what immuta_user_attribute needs.
output "jane_user_profile_id" {
  value = immuta_user.jane.id
}
