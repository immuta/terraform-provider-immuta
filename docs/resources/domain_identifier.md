---
page_title: "immuta_domain_identifier Resource - terraform-provider-immuta"
subcategory: ""
description: |-
  Copies an existing Immuta identifier into a domain with the documented bulk-copy API.
---

# immuta_domain_identifier (Resource)

Copies an existing identifier into an Immuta domain with
`POST /sdd/identifier/bulk-copy/{domainId}`, which is what the Immuta UI does
when you add an existing identifier to a domain.

~> **Immuta copies, it does not move.** The result is a brand new identifier
with its own ID that lives in the domain. The identifier it was copied from is
left exactly as it was. Editing the copy does not change the original, and
editing the original does not change the copy. They diverge from the moment
the copy is made.

To author a new identifier directly in a domain, set `domain_id` on
[`immuta_identifier`](identifier.md) instead.

## Example usage

```terraform
resource "immuta_domain_identifier" "finance_credit_card" {
  domain_id            = immuta_domain.finance.id
  source_identifier_id = immuta_identifier.credit_card.id
}
```

## Schema

### Required

- `domain_id` (String) ID of the domain to copy the identifier into. Changing
  it makes a new copy and deletes the old one.
- `source_identifier_id` (String) ID of the identifier to copy. Changing it
  makes a new copy and deletes the old one.

### Read-Only

- `id` (String) ID of the copy Immuta created inside the domain. This is not
  the same as `source_identifier_id`.
- `name` (String) Name of the copy, taken from the identifier it was copied
  from.
- `description` (String) Description of the copy.
- `type` (String) Criteria type of the copy.
- `tags` (Set of String) Fully qualified names of the tags the copy applies.
- `created_at` (String) Timestamp of when the copy was created.
- `updated_at` (String) Timestamp of when the copy was last updated.

## Behaviour notes

- **The copy is not managed beyond its existence.** Terraform tracks that the
  copy exists and what it looks like, but its definition comes from whatever
  the source looked like at copy time. Changing the source afterwards does not
  update the copy, and no diff is produced.
- **Nothing is updated in place.** Both arguments replace the copy, because
  copying is the only documented operation.
- **Errors Immuta reports are surfaced.** The bulk copy answers
  `{"count", "createdIdentifiers", "errors"}` and can return success while
  listing a failure in `errors`. The provider fails the apply with Immuta's
  message instead of recording a copy that was never made.
- **A failed apply can leave an orphaned copy.** If a copy is made but the
  apply fails afterwards, the copy stays in the domain untracked, and a later
  copy of the same identifier may then be rejected. Delete the orphan in
  Immuta, or import it, before applying again.
- **Remote changes show up as drift.** A copy deleted in the Immuta UI is
  removed from state and planned for recreation, which makes a fresh copy.

## Destructive behaviour

Destroying an `immuta_domain_identifier` deletes **only the copy**. The
identifier it was copied from is untouched.

The domain stops applying that identifier's tags to newly scanned columns.

## Import

Immuta does not record which identifier a copy came from, so both IDs are
needed:

```shell
terraform import immuta_domain_identifier.finance_credit_card 'a32c69df-3707-4b03-8144-a8d11d1f52a4/b71e4c02-9a11-4f3e-8c7d-55de2f0a1c98'
```

The first ID is the identifier that was copied, the second is the copy itself.
