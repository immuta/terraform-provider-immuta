#!/usr/bin/env bash
#
# Exercise the Immuta BIM identity endpoints with curl only, independently of
# Terraform. Use it to confirm connectivity, credentials and permissions
# before debugging the provider itself.
#
#   export IMMUTA_HOST="https://example.immuta.com"
#   export IMMUTA_API_KEY="..."
#   ./scripts/smoke_bim.sh
#
# Optional:
#   IMMUTA_TEST_BIM_PREFIX   name prefix for the throwaway user and group
#
# The script creates a group and a user, adds the user to the group, assigns
# an attribute to each, reads everything back, and then removes it all.
#
# All of these endpoints are documented apart from one: deleting a user with
# DELETE /bim/iam/{iamid}/user/{userid}. That request is what the Immuta UI
# issues for "permanently delete user" and is the single undocumented call the
# provider makes, so this script checks it explicitly. A failure there means
# immuta_user cannot be destroyed cleanly.
#
# Run it against a sandbox tenant, never production. The API key is only ever
# sent in a request header and is never printed.

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

PREFIX="${IMMUTA_TEST_BIM_PREFIX:-tf-smoke-immuta}"
SUFFIX="$(date +%Y%m%d%H%M%S)-$$"
GROUP_NAME="${PREFIX}-${SUFFIX}"
USER_NAME="${PREFIX}-${SUFFIX}@example.invalid"
ATTRIBUTE_NAME='SmokeTest'
ATTRIBUTE_VALUE='smoke value'

IAM_ID='bim'

WORK_DIR="$(mktemp -d)"
BODY_FILE="${WORK_DIR}/body.json"
GROUP_ID=''
USER_ID=''
MEMBERSHIP_ID=''
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

url_encode() {
  jq -rn --arg value "$1" '$value|@uri'
}

cleanup() {
  # Best effort: never leave an identity behind in the tenant.
  if [ -n "${USER_ID}" ]; then
    echo "==> Cleaning up user ${USER_ID}"
    api_request DELETE "/bim/iam/${IAM_ID}/user/$(url_encode "${USER_ID}")" >/dev/null 2>&1 || true
  fi

  if [ -n "${GROUP_ID}" ]; then
    echo "==> Cleaning up group ${GROUP_ID}"
    api_request DELETE "/bim/group/${GROUP_ID}" >/dev/null 2>&1 || true
  fi

  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

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

echo 'Immuta BIM identity smoke test'
echo "  host:  ${HOST}"
echo "  group: ${GROUP_NAME}"
echo "  user:  ${USER_NAME}"
echo

echo '==> POST /bim/group'
create_group_body="$(jq -n --arg name "${GROUP_NAME}" --arg iamid "${IAM_ID}" '{name: $name, iamid: $iamid, description: "Created by scripts/smoke_bim.sh"}')"
status="$(api_request POST '/bim/group' "${create_group_body}")"
if ! expect_status 'create the group' "${status}" 200 201; then
  echo
  echo 'FAIL: cannot continue without a created group'
  exit 1
fi

GROUP_ID="$(jq -r '.id // empty' "${BODY_FILE}")"
if [ -z "${GROUP_ID}" ]; then
  fail 'the create response contained a group id'
  echo
  echo 'FAIL: cannot continue without a group id'
  exit 1
fi
pass "the create response contained a group id (${GROUP_ID})"

echo '==> POST /bim/iam/bim/user'
create_user_body="$(jq -n --arg userid "${USER_NAME}" --arg iamid "${IAM_ID}" \
  '{iamid: $iamid, userid: $userid, profile: {name: "Immuta smoke test", email: $userid}, permissions: []}')"
status="$(api_request POST "/bim/iam/${IAM_ID}/user" "${create_user_body}")"
if ! expect_status 'create the user' "${status}" 200 201; then
  echo
  echo 'FAIL: cannot continue without a created user'
  exit 1
fi

USER_ID="${USER_NAME}"
USER_PROFILE_ID="$(jq -r '.newUser.id // .newUser.profile.id // empty' "${BODY_FILE}")"
if [ -z "${USER_PROFILE_ID}" ]; then
  fail 'the create response contained a numeric user id'
  echo
  echo 'FAIL: cannot continue without a user id'
  exit 1
fi
pass "the create response contained a numeric user id (${USER_PROFILE_ID})"

encoded_user="$(url_encode "${USER_ID}")"

echo '==> GET /bim/iam/{iamid}/user/{id}'
status="$(api_request GET "/bim/iam/${IAM_ID}/user/${USER_PROFILE_ID}")"
if expect_status 'read the user by numeric id' "${status}" 200; then
  expect_value 'the username round trips' "$(jq -r '.userid // empty' "${BODY_FILE}")" "${USER_ID}"
fi

echo '==> PUT /bim/iam/{iamid}/user/{userid}/profile'
status="$(api_request PUT "/bim/iam/${IAM_ID}/user/${encoded_user}/profile" '{"location":"Smoke test"}')"
expect_status 'update the profile' "${status}" 200 204 || true

echo '==> PUT /bim/iam/{iamid}/user/{userid}/permissions'
status="$(api_request PUT "/bim/iam/${IAM_ID}/user/${encoded_user}/permissions" '["CREATE_PROJECT"]')"
expect_status 'replace the permissions' "${status}" 200 204 || true

echo '==> POST /bim/group/{groupId}/user'
add_member_body="$(jq -n --arg userid "${USER_ID}" --arg iamid "${IAM_ID}" '{userid: $userid, iamid: $iamid}')"
status="$(api_request POST "/bim/group/${GROUP_ID}/user" "${add_member_body}")"
if expect_status 'add the user to the group' "${status}" 200 201; then
  MEMBERSHIP_ID="$(jq -r '.id // empty' "${BODY_FILE}")"
  if [ -n "${MEMBERSHIP_ID}" ]; then
    pass "the response contained a membership id (${MEMBERSHIP_ID})"
  else
    fail 'the response contained a membership id'
  fi
fi

echo '==> GET /bim/iam/{iamid}/user/{userid}/groups'
status="$(api_request GET "/bim/iam/${IAM_ID}/user/${encoded_user}/groups")"
if expect_status 'list the groups of the user' "${status}" 200; then
  # This is the only documented way to recover the membership id, which the
  # provider needs in order to remove the user from the group again.
  found="$(jq -r --arg group "${GROUP_ID}" '[.[] | select((.id|tostring) == $group) | .groupUser] | first // empty' "${BODY_FILE}")"
  if [ -n "${found}" ]; then
    pass "the group list exposes the membership id (${found})"
  else
    fail 'the group list exposes the membership id'
  fi
fi

encoded_attribute_name="$(url_encode "${ATTRIBUTE_NAME}")"
encoded_attribute_value="$(url_encode "${ATTRIBUTE_VALUE}")"

echo '==> PUT user authorizations'
status="$(api_request PUT "/bim/iam/${IAM_ID}/user/${encoded_user}/authorizations/${encoded_attribute_name}/${encoded_attribute_value}")"
expect_status 'assign an attribute to the user' "${status}" 200 204 || true

echo '==> GET /bim/iam/{iamid}/user/{id} after assigning the attribute'
status="$(api_request GET "/bim/iam/${IAM_ID}/user/${USER_PROFILE_ID}")"
if expect_status 'read the user back' "${status}" 200; then
  # The provider reads bimAuthorizations so that attributes inherited from a
  # group are not mistaken for attributes assigned to the user.
  held="$(jq -r --arg name "${ATTRIBUTE_NAME}" --arg value "${ATTRIBUTE_VALUE}" \
    '((.bimAuthorizations // .authorizations // {})[$name] // []) | index($value) != null' "${BODY_FILE}")"
  expect_value 'the attribute appears in bimAuthorizations' "${held}" 'true'
fi

echo '==> PUT group authorizations'
status="$(api_request PUT "/bim/iam/${IAM_ID}/group/${GROUP_ID}/authorizations/${encoded_attribute_name}/${encoded_attribute_value}")"
expect_status 'assign an attribute to the group' "${status}" 200 204 || true

echo '==> GET /bim/group/{groupId}'
status="$(api_request GET "/bim/group/${GROUP_ID}")"
if expect_status 'read the group back' "${status}" 200; then
  held="$(jq -r --arg name "${ATTRIBUTE_NAME}" --arg value "${ATTRIBUTE_VALUE}" \
    '((.authorizations // {})[$name] // []) | index($value) != null' "${BODY_FILE}")"
  expect_value 'the attribute appears on the group' "${held}" 'true'
fi

echo '==> DELETE user authorizations'
status="$(api_request DELETE "/bim/iam/${IAM_ID}/user/${encoded_user}/authorizations/${encoded_attribute_name}/${encoded_attribute_value}")"
expect_status 'remove the attribute from the user' "${status}" 200 204 || true

if [ -n "${MEMBERSHIP_ID}" ]; then
  echo '==> DELETE /bim/group/{groupId}/user/{groupuserid}'
  status="$(api_request DELETE "/bim/group/${GROUP_ID}/user/${MEMBERSHIP_ID}")"
  expect_status 'remove the user from the group' "${status}" 200 204 || true
fi

echo '==> DELETE /bim/iam/{iamid}/user/{userid}  (undocumented)'
status="$(api_request DELETE "/bim/iam/${IAM_ID}/user/${encoded_user}")"
if expect_status 'delete the user' "${status}" 200 204; then
  # Deleted, so the cleanup trap has nothing left to do.
  USER_ID=''
else
  echo '          This is the one undocumented endpoint the provider uses.'
  echo '          If it has changed, destroying an immuta_user will fail.'
fi

echo '==> GET /bim/iam/{iamid}/user/{id} after the delete'
status="$(api_request GET "/bim/iam/${IAM_ID}/user/${USER_PROFILE_ID}")"
expect_status 'the deleted user is gone' "${status}" 404 || true

echo '==> DELETE /bim/group/{groupId}'
deleted_group="${GROUP_ID}"
status="$(api_request DELETE "/bim/group/${GROUP_ID}")"
if expect_status 'delete the group' "${status}" 200 204; then
  GROUP_ID=''
fi

echo '==> GET /bim/group/{groupId} after the delete'
status="$(api_request GET "/bim/group/${deleted_group}")"
expect_status 'the deleted group is gone' "${status}" 404 || true

echo
if [ "${FAILURES}" -eq 0 ]; then
  echo 'PASS: the BIM identity endpoints behave as expected'
  exit 0
fi

echo "FAIL: ${FAILURES} check(s) failed"
exit 1
