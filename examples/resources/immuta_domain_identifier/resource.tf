resource "immuta_domain" "finance" {
  name        = "Finance"
  description = "Finance data domain"
}

resource "immuta_tag" "credit_card" {
  name = "Discovered.Credit card"
}

# A tenant-wide reference identifier, not tied to any domain.
resource "immuta_identifier" "credit_card" {
  name        = "Credit card"
  description = "Columns that hold a card number"
  type        = "regex"
  regex       = "[0-9]{16}"
  tags        = [immuta_tag.credit_card.name]
}

# Put the reference identifier to work inside the domain. Immuta copies it:
# the result is a new identifier with its own id, and the one above is left
# untouched. Destroying this resource deletes only the copy.
resource "immuta_domain_identifier" "finance_credit_card" {
  domain_id            = immuta_domain.finance.id
  source_identifier_id = immuta_identifier.credit_card.id
}

output "copied_identifier_id" {
  value = immuta_domain_identifier.finance_credit_card.id
}
