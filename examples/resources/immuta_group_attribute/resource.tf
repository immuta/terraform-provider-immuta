resource "immuta_group" "engineering" {
  name = "Engineering"
}

# Every member of the group inherits the attribute, so granting it here is
# usually better than assigning it to each user.
resource "immuta_group_attribute" "engineering_department" {
  group_id = immuta_group.engineering.id
  name     = "Department"
  value    = "Engineering"
}
