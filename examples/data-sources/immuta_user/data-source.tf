# Look a user up by email address.
data "immuta_user" "jane" {
  email = "jane.doe@immuta.com"
}

# Or by username, in a given identity manager. iam_id is needed when the
# same person exists in several identity managers.
data "immuta_user" "sam" {
  user_id = "sam.smith@contractor.example"
  iam_id  = "bim"
}

# id is the numeric profile ID as a string, like immuta_user.id. Documents
# built with yamlencode() or jsonencode() need it as a number.
locals {
  data_object_defaults = yamlencode({
    owners = [{ type = "USER", id = tonumber(data.immuta_user.jane.id) }]
  })
}
