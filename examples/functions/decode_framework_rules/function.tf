# Rules are usually kept in their own file:
#
#   rules = provider::immuta::decode_framework_rules(file("${path.module}/rules.yaml"))
#
# They are inlined here so that the example is self-contained.
locals {
  hr_rules_yaml = <<-EOT
    - name: Employee data
      classification_tag: { name: HR Framework.Internal Employee Data, source: curated }
      neighbor_column_tags:
        - { name: Discovered.Employee Name, source: curated }

    # Keys can also be spelt as in the Immuta API.
    - name: Salary
      classificationTag: { name: HR Framework.Salary, source: curated }
      columnTags:
        - { name: Discovered.Salary, source: curated }
  EOT
}

resource "immuta_framework" "hr" {
  short_name  = "HR Information"
  name        = "HR Information Framework"
  description = "Finds internal employee information and tags it for HR."
  active      = true

  tags = [
    { name = "HR Framework.Internal Employee Data", source = "curated" },
    { name = "HR Framework.Salary", source = "curated" },
  ]

  rules = provider::immuta::decode_framework_rules(local.hr_rules_yaml)
}
