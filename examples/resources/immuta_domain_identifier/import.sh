# Immuta does not record which identifier a copy came from, so import needs
# both: {source_identifier_id}/{copy_id}.
terraform import immuta_domain_identifier.finance_credit_card 'a32c69df-3707-4b03-8144-a8d11d1f52a4/b71e4c02-9a11-4f3e-8c7d-55de2f0a1c98'
