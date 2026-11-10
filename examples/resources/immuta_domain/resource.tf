resource "immuta_domain" "research" {
  name         = "Research"
  description  = "Domain for the research organisation"
  autoscanning = true
}

# Server managed attributes are available for downstream use.
output "research_domain_id" {
  value = immuta_domain.research.id
}

output "research_domain_assignment_type" {
  value = immuta_domain.research.assignment_type
}
