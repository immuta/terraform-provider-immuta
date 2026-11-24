resource "immuta_user" "jane" {
  user_id = "jane.doe@immuta.com"
  name    = "Jane Doe"
}

resource "immuta_group" "engineering" {
  name = "Engineering"
}

# One resource per membership, so memberships created elsewhere are untouched.
resource "immuta_group_user" "jane_engineering" {
  group_id = immuta_group.engineering.id
  user_id  = immuta_user.jane.user_id
}

# Add a whole team to the same group.
resource "immuta_group_user" "platform_team" {
  for_each = toset(["ada.lovelace@immuta.com", "alan.turing@immuta.com"])

  group_id = immuta_group.engineering.id
  user_id  = each.value
}

# Users from an external identity manager are looked up rather than managed.
data "immuta_user" "grace" {
  email = "grace.hopper@immuta.com"
}

resource "immuta_group_user" "grace_engineering" {
  group_id = immuta_group.engineering.id
  iam_id   = data.immuta_user.grace.iam_id
  user_id  = data.immuta_user.grace.user_id
}
