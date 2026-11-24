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

// TestAccFrameworkResource exercises the documented framework lifecycle
// against a real Immuta tenant. It only runs when TF_ACC is set, and it
// requires IMMUTA_HOST, IMMUTA_API_KEY and IMMUTA_TEST_DOMAIN_NAME_PREFIX. The
// API key must hold the GOVERNANCE permission.
//
// Every tag the framework references is created with immuta_tag first, so
// the test leaves nothing behind.
//
// Run it against a sandbox tenant, never production.
func TestAccFrameworkResource(t *testing.T) {
	name := acctest.RandomWithPrefix(accNamePrefix(t))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutil.RequireAccEnv(t) },
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             testAccCheckFrameworksDestroyed(t),
		Steps: []resource.TestStep{
			{
				Config: testAccFrameworkConfig(name, "Created by the provider acceptance tests", true, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(frameworkResourceName, "id"),
					resource.TestCheckResourceAttr(frameworkResourceName, "name", name),
					resource.TestCheckResourceAttr(frameworkResourceName, "rules.#", "1"),
					testAccCheckFrameworkParity(t),
				),
			},
			{
				// A second plan must report no changes.
				Config:   testAccFrameworkConfig(name, "Created by the provider acceptance tests", true, true),
				PlanOnly: true,
			},
			{
				ResourceName:      frameworkResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Deactivate, change the description and drop the sensitivity.
				Config: testAccFrameworkConfig(name, "Updated by the provider acceptance tests", false, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(frameworkResourceName, "active", "false"),
					resource.TestCheckResourceAttr(frameworkResourceName, "description", "Updated by the provider acceptance tests"),
					testAccCheckFrameworkParity(t),
				),
			},
			{
				Config:   testAccFrameworkConfig(name, "Updated by the provider acceptance tests", false, false),
				PlanOnly: true,
			},
		},
	})
}

func testAccFrameworkConfig(name, description string, active, sensitive bool) string {
	sensitivities := ""
	if sensitive {
		sensitivities = `
      sensitivities = [{ dimension = "confidentiality", sensitivity = 1 }]`
	}

	return fmt.Sprintf(`
resource "immuta_tag" "classification" {
  name = "%[1]s.Internal"
}

resource "immuta_tag" "criteria" {
  name = "%[1]s.Employee Name"
}

resource "immuta_framework" "test" {
  short_name  = %[1]q
  name        = %[1]q
  description = %[2]q
  active      = %[3]t

  tags = [
    {
      name   = immuta_tag.classification.name
      source = "curated"%[4]s
    },
  ]

  rules = [
    {
      name               = "Employee data"
      classification_tag = { name = immuta_tag.classification.name, source = "curated" }
      neighbor_column_tags = [
        { name = immuta_tag.criteria.name, source = "curated" },
      ]
    },
  ]
}
`, name, description, active, sensitivities)
}

// testAccCheckFrameworkParity reads the framework straight from the Immuta
// API and compares it with Terraform state.
func testAccCheckFrameworkParity(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[frameworkResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", frameworkResourceName)
		}

		framework, err := testutil.AccClient(t).GetFramework(context.Background(), resourceState.Primary.ID)
		if err != nil {
			return fmt.Errorf("reading framework %s from Immuta: %w", resourceState.Primary.ID, err)
		}

		return testutil.FrameworkParityError(framework, resourceState.Primary.Attributes)
	}
}

// testAccCheckFrameworksDestroyed asserts that every managed framework is
// gone from the tenant once the test has finished.
func testAccCheckFrameworksDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		client := testutil.AccClient(t)

		for name, resourceState := range state.RootModule().Resources {
			if resourceState.Type != "immuta_framework" {
				continue
			}

			_, err := client.GetFramework(context.Background(), resourceState.Primary.ID)
			switch {
			case err == nil:
				return fmt.Errorf("%s (%s) still exists in Immuta after destroy", name, resourceState.Primary.ID)
			case !immutaapi.IsNotFound(err):
				return fmt.Errorf("reading framework %s from Immuta: %w", resourceState.Primary.ID, err)
			}
		}

		return nil
	}
}
