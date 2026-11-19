#!/usr/bin/env bash
#
# Exercise the documented Immuta domain endpoints with curl only, independently
# of Terraform. Use it to confirm connectivity, credentials and permissions
# before debugging the provider itself.
#
#   export IMMUTA_HOST="https://example.immuta.com"
#   export IMMUTA_API_KEY="..."
#   ./scripts/smoke_domain.sh
#
# Optional:
#   IMMUTA_TEST_DOMAIN_NAME_PREFIX   name prefix for the throwaway domain
#
# The script creates a domain, reads it by id and by name, updates it, deletes
# it, and confirms it is gone. Run it against a sandbox tenant, never
# production. The API key is only ever sent in a request header and is never
# printed.

set -euo pipefail

if [ -z "${IMMUTA_HOST:-}" ]; then
  echo "FAIL: IMMUTA_HOST is not set" >&2
  exit 2
fi

if [ -z "${IMMUTA_API_KEY:-}" ]; then
  echo "FAIL: IMMUTA_API_KEY is not set" >&2
  exit 2
fi

for tool in curl jq; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "FAIL: ${tool} is required but not installed" >&2
    exit 2
  fi
done

# Accept a bare hostname as well as a full URL, and drop any trailing slash.
HOST="${IMMUTA_HOST}"
case "${HOST}" in
  http://*|https://*) ;;
  *) HOST="https://${HOST}" ;;
esac
HOST="${HOST%/}"

PREFIX="${IMMUTA_TEST_DOMAIN_NAME_PREFIX:-tf-smoke-immuta}"
DOMAIN_NAME="${PREFIX}-$(date +%Y%m%d%H%M%S)-$$"
DESCRIPTION="Created by scripts/smoke_domain.sh"
UPDATED_DESCRIPTION="Updated by scripts/smoke_domain.sh"

WORK_DIR="$(mktemp -d)"
BODY_FILE="${WORK_DIR}/body.json"
DOMAIN_ID=""
FAILURES=0

# api_request METHOD PATH [JSON_BODY] -> prints the HTTP status, body in BODY_FILE
api_request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"

  if [ -n "${body}" ]; then
    curl --silent --show-error --output "${BODY_FILE}" --write-out '%{http_code}' \
      --request "${method}" \
      --header "Authorization: ${IMMUTA_API_KEY}" \
      --header 'Accept: application/json' \
      --header 'Content-Type: application/json' \
      --data "${body}" \
      "${HOST}${path}"
  else
    curl --silent --show-error --output "${BODY_FILE}" --write-out '%{http_code}' \
      --request "${method}" \
      --header "Authorization: ${IMMUTA_API_KEY}" \
      --header 'Accept: application/json' \
      "${HOST}${path}"
  fi
}

cleanup() {
  # Best effort: never leave a domain behind in the tenant.
  if [ -n "${DOMAIN_ID}" ]; then
    echo "==> Cleaning up domain ${DOMAIN_ID}"
    api_request DELETE "/domain/${DOMAIN_ID}" >/dev/null 2>&1 || true
  fi

  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

# The documented responses use both a bare object and a data envelope.
domain_field() {
  jq -r --arg field "$1" '(.data // .) | if type == "array" then .[0] else . end | .[$field] // empty' "${BODY_FILE}" 2>/dev/null || true
}

pass() {
  echo "    PASS: $1"
}

fail() {
  echo "    FAIL: $1"
  FAILURES=$((FAILURES + 1))
}

expect_status() {
  local description="$1"
  local actual="$2"
  shift 2

  local expected
  for expected in "$@"; do
    if [ "${actual}" = "${expected}" ]; then
      pass "${description} (HTTP ${actual})"
      return 0
    fi
  done

  fail "${description} (HTTP ${actual}, wanted one of: $*)"

  return 1
}

expect_value() {
  local description="$1"
  local actual="$2"
  local expected="$3"

  if [ "${actual}" = "${expected}" ]; then
    pass "${description}"
  else
    fail "${description} (got '${actual}', wanted '${expected}')"
  fi
}

echo 'Immuta domain smoke test'
echo "  host:   ${HOST}"
echo "  domain: ${DOMAIN_NAME}"
echo

echo '==> POST /domain'
create_body="$(jq -n --arg name "${DOMAIN_NAME}" --arg description "${DESCRIPTION}" '{name: $name, description: $description, type: "domain"}')"
status="$(api_request POST '/domain' "${create_body}")"
if ! expect_status 'create the domain' "${status}" 200 201; then
  echo
  echo 'FAIL: cannot continue without a created domain'
  exit 1
fi

DOMAIN_ID="$(domain_field id)"
if [ -z "${DOMAIN_ID}" ]; then
  fail 'the create response contained a domain id'
  echo
  echo 'FAIL: cannot continue without a domain id'
  exit 1
fi
pass "the create response contained a domain id (${DOMAIN_ID})"

echo '==> GET /domain/{domainId}'
status="$(api_request GET "/domain/${DOMAIN_ID}")"
if expect_status 'read the domain by id' "${status}" 200; then
  expect_value 'the name round trips' "$(domain_field name)" "${DOMAIN_NAME}"
  expect_value 'the description round trips' "$(domain_field description)" "${DESCRIPTION}"
  expect_value 'the type is domain' "$(domain_field type)" 'domain'
fi

echo '==> GET /domain/name/{domainName}'
encoded_name="$(jq -rn --arg name "${DOMAIN_NAME}" '$name|@uri')"
status="$(api_request GET "/domain/name/${encoded_name}")"
if expect_status 'read the domain by name' "${status}" 200; then
  expect_value 'the same domain is returned' "$(domain_field id)" "${DOMAIN_ID}"
fi

echo '==> PUT /domain/{domainId}'
update_body="$(jq -n --arg description "${UPDATED_DESCRIPTION}" '{description: $description}')"
status="$(api_request PUT "/domain/${DOMAIN_ID}" "${update_body}")"
expect_status 'update the description' "${status}" 200 204 || true

echo '==> GET /domain/{domainId} after the update'
status="$(api_request GET "/domain/${DOMAIN_ID}")"
if expect_status 'read the domain back' "${status}" 200; then
  expect_value 'the updated description was stored' "$(domain_field description)" "${UPDATED_DESCRIPTION}"
fi

echo '==> DELETE /domain/{domainId}'
deleted_id="${DOMAIN_ID}"
status="$(api_request DELETE "/domain/${DOMAIN_ID}")"
if expect_status 'delete the domain' "${status}" 200 204; then
  # Deleted, so the cleanup trap has nothing left to do.
  DOMAIN_ID=''
fi

echo '==> GET /domain/{domainId} after the delete'
status="$(api_request GET "/domain/${deleted_id}")"
expect_status 'the deleted domain is gone' "${status}" 404 || true

echo
if [ "${FAILURES}" -eq 0 ]; then
  echo 'PASS: the documented domain endpoints behave as expected'
  exit 0
fi

echo "FAIL: ${FAILURES} check(s) failed"
exit 1
