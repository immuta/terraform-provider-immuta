resource "immuta_domain" "denmark" {
  name = "Denmark"
}

# Every data source tagged with any of these tags is in the domain, now and
# as data sources are tagged later.
resource "immuta_domain_dynamic_assignment" "denmark" {
  domain_id = immuta_domain.denmark.id
  tags = [
    "Discovered.Country.Denmark",
  ]
}
