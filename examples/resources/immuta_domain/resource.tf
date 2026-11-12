resource "immuta_domain" "research" {
  name         = "Research"
  description  = "Domain for the research organisation"
  autoscanning = true

  # Set to true to remove every data source from the domain when destroying it.
  # Apply the change before the destroy that relies on it.
  force_destroy = false
}

# Server managed attributes are available for downstream use.
output "research_domain_id" {
  value = immuta_domain.research.id
}

output "research_domain_assignment_type" {
  value = immuta_domain.research.assignment_type
}
