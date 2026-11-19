# Memberships are imported with {iam_id}/{user_id}/{group_id}. The provider
# looks the membership id up from GET /bim/iam/{iamid}/user/{userid}/groups
# during the first read, so it does not need to be supplied.
terraform import immuta_group_user.jane_engineering 'bim/jane.doe@immuta.com/12'
