resource "immuta_user" "jane" {
  user_id = "jane.doe@immuta.com"
  name    = "Jane Doe"
}

# Immuta takes the username on the attribute write endpoints and the numeric
# profile id on the user read endpoint, so both are required.
resource "immuta_user_attribute" "jane_department" {
  user_id         = immuta_user.jane.user_id
  user_profile_id = immuta_user.jane.id
  name            = "Department"
  value           = "Finance"
}

# One resource holds one value. Use for_each to assign several values of the
# same attribute.
resource "immuta_user_attribute" "jane_regions" {
  for_each = toset(["APAC", "EMEA"])

  user_id         = immuta_user.jane.user_id
  user_profile_id = immuta_user.jane.id
  name            = "Region"
  value           = each.value
}

# Users from an external identity manager are looked up rather than managed.
data "immuta_user" "ada" {
  email = "ada.lovelace@immuta.com"
}

resource "immuta_user_attribute" "ada_department" {
  iam_id          = data.immuta_user.ada.iam_id
  user_id         = data.immuta_user.ada.user_id
  user_profile_id = data.immuta_user.ada.id
  name            = "Department"
  value           = "Engineering"
}
