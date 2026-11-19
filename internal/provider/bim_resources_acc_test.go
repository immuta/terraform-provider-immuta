package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// TestAccBIMIdentityResources exercises the user, group, membership and
// attribute lifecycle against a real Immuta tenant. It only runs when TF_ACC
// is set, and it requires IMMUTA_HOST, IMMUTA_API_KEY and
// IMMUTA_TEST_DOMAIN_NAME_PREFIX.
//
// Destroying immuta_user permanently deletes the user, so run this against a
// sandbox tenant, never production.
func TestAccBIMIdentityResources(t *testing.T) {
	prefix := accNamePrefix(t)
	groupName := acctest.RandomWithPrefix(prefix)
	username := acctest.RandomWithPrefix(prefix) + "@example.invalid"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutil.RequireAccEnv(t) },
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             testAccCheckBIMIdentitiesDestroyed(t),
		Steps: []resource.TestStep{
			{
				Config: testAccBIMConfig(username, groupName, "Created by the provider acceptance tests", "Finance"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(userResourceName, "id"),
					resource.TestCheckResourceAttr(userResourceName, "user_id", username),
					resource.TestCheckResourceAttr(userResourceName, "disabled", "false"),
					resource.TestCheckResourceAttrSet(groupResourceName, "id"),
					resource.TestCheckResourceAttr(groupResourceName, "name", groupName),
					resource.TestCheckResourceAttrSet(groupUserResourceName, "membership_id"),
					resource.TestCheckResourceAttr(userAttributeResourceName, "value", "Finance"),
					testAccCheckUserParity(t),
					testAccCheckGroupParity(t),
				),
			},
			{
				// A second plan must report no changes.
				Config:   testAccBIMConfig(username, groupName, "Created by the provider acceptance tests", "Finance"),
				PlanOnly: true,
			},
			{
				// Import into a separate state and compare every attribute.
				ResourceName:      userResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      groupResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      groupUserResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      userAttributeResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Updates are applied in place, and changing an attribute
				// value replaces only that assignment.
				Config: testAccBIMConfig(username, groupName, "Updated by the provider acceptance tests", "Legal"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(groupResourceName, "description", "Updated by the provider acceptance tests"),
					resource.TestCheckResourceAttr(userAttributeResourceName, "value", "Legal"),
					testAccCheckUserParity(t),
					testAccCheckGroupParity(t),
				),
			},
		},
	})
}

func testAccBIMConfig(username, groupName, description, attributeValue string) string {
	return fmt.Sprintf(`
resource "immuta_user" "test" {
  user_id = %[1]q
  name    = "Provider acceptance test"
}

resource "immuta_group" "test" {
  name        = %[2]q
  description = %[3]q
}

resource "immuta_group_user" "test" {
  group_id = immuta_group.test.id
  user_id  = immuta_user.test.user_id
}

resource "immuta_user_attribute" "test" {
  user_id         = immuta_user.test.user_id
  user_profile_id = immuta_user.test.id
  name            = "Department"
  value           = %[4]q
}

resource "immuta_group_attribute" "test" {
  group_id = immuta_group.test.id
  name     = "Department"
  value    = "Engineering"
}
`, username, groupName, description, attributeValue)
}

func testAccCheckUserParity(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[userResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", userResourceName)
		}

		id, err := strconv.ParseInt(resourceState.Primary.ID, 10, 64)
		if err != nil {
			return fmt.Errorf("the user ID %q is not numeric: %w", resourceState.Primary.ID, err)
		}

		user, err := testutil.AccClient(t).GetUser(context.Background(), resourceState.Primary.Attributes["iam_id"], id)
		if err != nil {
			return fmt.Errorf("reading user %d from Immuta: %w", id, err)
		}

		return testutil.UserParityError(user, resourceState.Primary.Attributes)
	}
}

func testAccCheckGroupParity(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[groupResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", groupResourceName)
		}

		id, err := strconv.ParseInt(resourceState.Primary.ID, 10, 64)
		if err != nil {
			return fmt.Errorf("the group ID %q is not numeric: %w", resourceState.Primary.ID, err)
		}

		group, err := testutil.AccClient(t).GetGroup(context.Background(), id)
		if err != nil {
			return fmt.Errorf("reading group %d from Immuta: %w", id, err)
		}

		return testutil.GroupParityError(group, resourceState.Primary.Attributes)
	}
}

// testAccCheckBIMIdentitiesDestroyed asserts that destroy removed the user
// and the group from the tenant. The user check is what proves the
// undocumented delete endpoint actually deleted the account rather than only
// disabling it.
func testAccCheckBIMIdentitiesDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		client := testutil.AccClient(t)

		for name, resourceState := range state.RootModule().Resources {
			id, err := strconv.ParseInt(resourceState.Primary.ID, 10, 64)
			if err != nil && resourceState.Type != "immuta_group_user" {
				// Composite identifiers are checked through their parents.
				if !strings.Contains(resourceState.Primary.ID, "/") {
					return fmt.Errorf("the %s ID %q is not numeric: %w", name, resourceState.Primary.ID, err)
				}

				continue
			}

			switch resourceState.Type {
			case "immuta_user":
				_, err = client.GetUser(context.Background(), immutaapi.IAMIDBIM, id)
			case "immuta_group":
				_, err = client.GetGroup(context.Background(), id)
			default:
				continue
			}

			switch {
			case err == nil:
				return fmt.Errorf("%s (%s) still exists in Immuta after destroy", name, resourceState.Primary.ID)
			case !immutaapi.IsNotFound(err):
				return fmt.Errorf("reading %s from Immuta: %w", name, err)
			}
		}

		return nil
	}
}
