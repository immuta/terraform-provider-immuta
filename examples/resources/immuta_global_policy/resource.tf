# Every YAML file in policies/ is one global policy, in the Immuta V2 policy
# format. Adding a file adds a policy and deleting a file deletes it.
resource "immuta_global_policy" "policies" {
  for_each = fileset("${path.module}/policies", "*.yaml")

  policy = file("${path.module}/policies/${each.value}")
}

# A single policy can also be managed on its own.
resource "immuta_global_policy" "hashing" {
  policy = <<-EOT
    name: Hashing (single)
    policyKey: data mask hashing single
    type: data
    actions:
      - rules:
          - type: Masking
            config:
              fields:
                - type: noTags
              maskingConfig:
                type: Hash
    circumstances:
      - type: noTags
  EOT
}
