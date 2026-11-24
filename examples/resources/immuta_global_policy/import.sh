# Global policies are imported with the id Immuta assigned them. The policy
# document cannot be read back in the V2 format, so the first apply after the
# import saves the configured document.
terraform import 'immuta_global_policy.policies["data-mask-hashing.yaml"]' 42
