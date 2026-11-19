resource "immuta_tag" "internal_employee_data" {
  name = "HR Framework.Internal Employee Data"
}

resource "immuta_tag" "employee_name" {
  name = "Discovered.Employee Name"
}

resource "immuta_framework" "hr" {
  short_name  = "HR Information"
  name        = "HR Information Framework"
  description = "Finds internal employee information and tags it for HR."
  active      = true

  tags = [
    {
      name   = immuta_tag.internal_employee_data.name
      source = "curated"
      sensitivities = [
        { dimension = "confidentiality", sensitivity = 1 },
      ]
    },
  ]

  rules = [
    {
      name               = "Employee data"
      classification_tag = { name = immuta_tag.internal_employee_data.name, source = "curated" }
      neighbor_column_tags = [
        { name = immuta_tag.employee_name.name, source = "curated" },
      ]
    },
  ]
}
