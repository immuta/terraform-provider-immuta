#!/usr/bin/env bash
#
# Validate the example configurations against a locally built provider.
#
# The provider is not published, so Terraform cannot download it. The script
# builds it, points Terraform at the build with a dev override, and validates
# every example. Example fragments that have no terraform block of their own
# are validated together with examples/provider/provider.tf.
#
# No Immuta tenant and no credentials are required.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROVIDER_SOURCE="immuta/immuta"

if ! command -v terraform >/dev/null 2>&1; then
  echo "terraform is not installed, skipping example validation"
  exit 0
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

BIN_DIR="${WORK_DIR}/bin"
mkdir -p "${BIN_DIR}"

echo "==> Building the provider"
(cd "${REPO_ROOT}" && go build -o "${BIN_DIR}/terraform-provider-immuta" .)

CLI_CONFIG="${WORK_DIR}/terraformrc"
cat > "${CLI_CONFIG}" <<TFRC
provider_installation {
  dev_overrides {
    "${PROVIDER_SOURCE}" = "${BIN_DIR}"
  }

  direct {}
}
TFRC

export TF_CLI_CONFIG_FILE="${CLI_CONFIG}"
export TF_IN_AUTOMATION=1

failed=0

validate_directory() {
  local example_dir="$1"
  local staging_dir="${WORK_DIR}/stage/$(echo "${example_dir}" | tr '/' '_')"

  mkdir -p "${staging_dir}"
  # Copy the whole directory, so that files such as YAML policies read with
  # file() or fileset() are validated too.
  cp -R "${example_dir}/." "${staging_dir}/"

  # Fragments such as examples/resources/<name>/resource.tf do not declare the
  # provider, so pair them with the provider example.
  if ! grep -rqs 'required_providers' "${staging_dir}"; then
    cp "${REPO_ROOT}/examples/provider/provider.tf" "${staging_dir}/"
  fi

  echo "==> Validating ${example_dir#"${REPO_ROOT}/"}"
  if terraform -chdir="${staging_dir}" validate -no-color; then
    echo "    PASS"
  else
    echo "    FAIL"
    failed=1
  fi
}

while IFS= read -r example_dir; do
  validate_directory "${example_dir}"
done < <(find "${REPO_ROOT}/examples" -name '*.tf' -exec dirname {} \; | sort -u)

if [ "${failed}" -ne 0 ]; then
  echo "FAIL: at least one example is not valid"
  exit 1
fi

echo "PASS: every example is valid"
