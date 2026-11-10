# Tags are hierarchical. Creating this tag also creates the parent
# "Discovered" when it does not already exist.
resource "immuta_tag" "primary_color" {
  name = "Discovered.Primary color"
}

# An identifier can only apply a tag that already exists. Referencing
# immuta_tag.primary_color.name rather than typing the string is what makes
# Terraform create the tag first.
resource "immuta_identifier" "primary_color" {
  name        = "Primary colors"
  description = "Columns that hold a primary color"
  type        = "regex"
  regex       = "(red|green|blue)"
  tags        = [immuta_tag.primary_color.name]
}

# Deleting a parent tag fails while it still has children, unless the whole
# hierarchy is removed with it.
resource "immuta_tag" "retired" {
  name             = "Retired"
  delete_hierarchy = true
}
