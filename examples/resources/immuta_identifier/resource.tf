resource "immuta_domain" "finance" {
  name        = "Finance"
  description = "Finance data domain"
}

resource "immuta_tag" "primary_color" {
  name = "Discovered.Primary color"
}

resource "immuta_tag" "credit_card" {
  name = "Discovered.Credit card"
}

# A regex identifier matches column values.
resource "immuta_identifier" "primary_color" {
  name        = "Primary colors"
  description = "Columns that hold a primary color"
  domain_id   = immuta_domain.finance.id
  type        = "regex"
  regex       = "(red|green|blue)"
  tags        = [immuta_tag.primary_color.name]
}

# A columnNameRegex identifier matches column names instead of their values.
resource "immuta_identifier" "credit_card_column" {
  name        = "Credit card columns"
  description = "Columns whose name suggests they hold a card number"
  type        = "columnNameRegex"
  regex       = "(card|pan)_?(number|no)"
  tags        = [immuta_tag.credit_card.name]
}

# A dictionary identifier matches column values against a fixed list.
resource "immuta_identifier" "primary_color_dictionary" {
  name           = "Primary color names"
  description    = "Columns that hold one of the primary colors by name"
  type           = "dictionary"
  values         = ["red", "green", "blue"]
  case_sensitive = false
  tags           = [immuta_tag.primary_color.name]
}
