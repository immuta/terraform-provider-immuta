package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// TestAccDomainResource exercises the documented domain lifecycle against a
// real Immuta tenant. It only runs when TF_ACC is set, and it requires
// IMMUTA_HOST, IMMUTA_API_KEY and IMMUTA_TEST_DOMAIN_NAME_PREFIX.
//
// Run it against a sandbox tenant, never production.
func TestAccDomainResource(t *testing.T) {
	name := acctest.RandomWithPrefix(accNamePrefix(t))
	renamed := name + " renamed"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutil.RequireAccEnv(t) },
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             testAccCheckDomainsDestroyed(t),
		Steps: []resource.TestStep{
			{
				Config: testAccDomainConfig(name, "Created by the provider acceptance tests"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(domainResourceName, "id"),
					resource.TestCheckResourceAttr(domainResourceName, "name", name),
					resource.TestCheckResourceAttr(domainResourceName, "description", "Created by the provider acceptance tests"),
					resource.TestCheckResourceAttr(domainResourceName, "type", "domain"),
					testAccCheckDomainParity(t),
				),
			},
			{
				// A second plan must report no changes.
				Config:   testAccDomainConfig(name, "Created by the provider acceptance tests"),
				PlanOnly: true,
			},
			{
				// Import into a separate state and compare every attribute.
				ResourceName:      domainResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccDomainConfig(renamed, "Updated by the provider acceptance tests"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "name", renamed),
					resource.TestCheckResourceAttr(domainResourceName, "description", "Updated by the provider acceptance tests"),
					testAccCheckDomainParity(t),
				),
			},
			{
				Config: fmt.Sprintf(`
resource "immuta_domain" "test" {
  name = %q
}
`, renamed),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(domainResourceName, "description"),
					testAccCheckDomainParity(t),
				),
			},
		},
	})
}

// accNamePrefix returns the configured test name prefix, or a placeholder when
// the acceptance tests are going to be skipped anyway.
func accNamePrefix(t *testing.T) string {
	t.Helper()

	if prefix := os.Getenv(testutil.EnvNamePrefix); prefix != "" {
		return prefix
	}

	return "tf-acc-immuta"
}

func testAccDomainConfig(name, description string) string {
	return fmt.Sprintf(`
resource "immuta_domain" "test" {
  name        = %q
  description = %q
}
`, name, description)
}

// testAccCheckDomainParity reads the domain straight from the Immuta API and
// compares it with Terraform state.
func testAccCheckDomainParity(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[domainResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", domainResourceName)
		}

		domain, err := testutil.AccClient(t).GetDomain(context.Background(), resourceState.Primary.ID)
		if err != nil {
			return fmt.Errorf("reading domain %s from Immuta: %w", resourceState.Primary.ID, err)
		}

		return testutil.DomainParityError(domain, resourceState.Primary.Attributes)
	}
}

// testAccCheckDomainsDestroyed asserts that every managed domain is gone from
// the tenant once the test has finished.
func testAccCheckDomainsDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		client := testutil.AccClient(t)

		for name, resourceState := range state.RootModule().Resources {
			if resourceState.Type != "immuta_domain" {
				continue
			}

			_, err := client.GetDomain(context.Background(), resourceState.Primary.ID)
			switch {
			case err == nil:
				return fmt.Errorf("%s (%s) still exists in Immuta after destroy", name, resourceState.Primary.ID)
			case !immutaapi.IsNotFound(err):
				return fmt.Errorf("reading domain %s from Immuta: %w", resourceState.Primary.ID, err)
			}
		}

		return nil
	}
}
