package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// TestAccGlobalPolicyResource exercises a masking and a subscription policy
// against a real Immuta tenant. It only runs when TF_ACC is set, and it
// requires IMMUTA_HOST, IMMUTA_API_KEY and IMMUTA_TEST_DOMAIN_NAME_PREFIX. The
// API key must hold the GOVERNANCE permission.
//
// Both policies are staged, so Immuta stores them without enforcing them on
// any data source. Run it against a sandbox tenant, never production.
func TestAccGlobalPolicyResource(t *testing.T) {
	name := acctest.RandomWithPrefix(accNamePrefix(t))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutil.RequireAccEnv(t) },
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             testAccCheckGlobalPoliciesDestroyed(t),
		Steps: []resource.TestStep{
			{
				Config: testAccGlobalPolicyConfig(name, "Hash"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckGlobalPolicyParity(t, "immuta_global_policy.masking"),
					testAccCheckGlobalPolicyParity(t, "immuta_global_policy.subscription"),
				),
			},
			{
				Config:   testAccGlobalPolicyConfig(name, "Hash"),
				PlanOnly: true,
			},
			{
				ResourceName:            "immuta_global_policy.masking",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"policy"},
			},
			{
				// Changing the masking updates the policy in place.
				Config: testAccGlobalPolicyConfig(name, "Null"),
				Check:  testAccCheckGlobalPolicyParity(t, "immuta_global_policy.masking"),
			},
			{
				Config:   testAccGlobalPolicyConfig(name, "Null"),
				PlanOnly: true,
			},
		},
	})
}

func testAccGlobalPolicyConfig(name, maskingType string) string {
	return fmt.Sprintf(`
resource "immuta_global_policy" "masking" {
  policy = <<-EOT
    name: %[1]s masking
    policyKey: %[1]s masking
    type: data
    staged: true
    actions:
      - rules:
          - type: Masking
            config:
              fields:
                - type: noTags
              maskingConfig:
                type: %[2]s
    circumstances:
      - type: noTags
  EOT
}

resource "immuta_global_policy" "subscription" {
  policy = <<-EOT
    name: %[1]s subscription
    policyKey: %[1]s subscription
    type: subscription
    staged: true
    actions:
      type: manual
      description: Created by the provider acceptance tests
    circumstances:
      - type: columnRegex
        regex: %[1]s
        caseInsensitive: false
  EOT
}
`, name, maskingType)
}

// testAccCheckGlobalPolicyParity reads the policy from the Immuta API and
// compares its key, name and type with Terraform state.
func testAccCheckGlobalPolicyParity(t *testing.T, address string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s is not in state", address)
		}

		policy, err := testutil.AccClient(t).GetGlobalPolicy(context.Background(), resourceState.Primary.ID)
		if err != nil {
			return fmt.Errorf("reading global policy %s from Immuta: %w", resourceState.Primary.ID, err)
		}

		attributes := resourceState.Primary.Attributes
		for attribute, remote := range map[string]string{"policy_key": policy.PolicyKey, "name": policy.Name, "type": policy.Type} {
			if attributes[attribute] != remote {
				return fmt.Errorf("%s %s = %q in state but %q in Immuta", address, attribute, attributes[attribute], remote)
			}
		}

		return nil
	}
}

// testAccCheckGlobalPoliciesDestroyed asserts that every managed policy is
// gone from the tenant once the test has finished.
func testAccCheckGlobalPoliciesDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		client := testutil.AccClient(t)

		for name, resourceState := range state.RootModule().Resources {
			if resourceState.Type != "immuta_global_policy" {
				continue
			}

			_, err := client.GetGlobalPolicy(context.Background(), resourceState.Primary.ID)
			switch {
			case err == nil:
				return fmt.Errorf("%s (%s) still exists in Immuta after destroy", name, resourceState.Primary.ID)
			case !immutaapi.IsNotFound(err):
				return fmt.Errorf("reading global policy %s from Immuta: %w", resourceState.Primary.ID, err)
			}
		}

		return nil
	}
}
