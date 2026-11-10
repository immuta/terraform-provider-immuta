# Users are imported with the numeric profile id Immuta assigned them, which is
# the id field returned by GET /bim/user. The IAM id may be given as a prefix.
terraform import immuta_user.jane 18
terraform import immuta_user.jane bim/18
