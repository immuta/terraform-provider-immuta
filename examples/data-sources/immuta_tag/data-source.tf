# Look a tag up by its fully qualified name, with dots between parent and
# child tags.
data "immuta_tag" "gold" {
  name = "Access.Layer.Gold"
}

output "gold_tag_id" {
  value = data.immuta_tag.gold.id
}
