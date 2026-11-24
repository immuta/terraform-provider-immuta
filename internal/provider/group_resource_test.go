package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const groupResourceName = "immuta_group.test"

func TestGroupResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGroupsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_group" "test" {
  name        = "Engineering"
  description = "Engineering department"
  email       = "engineering@immuta.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(groupResourceName, "id"),
					resource.TestCheckResourceAttr(groupResourceName, "iam_id", "bim"),
					resource.TestCheckResourceAttr(groupResourceName, "name", "Engineering"),
					resource.TestCheckResourceAttr(groupResourceName, "description", "Engineering department"),
					resource.TestCheckResourceAttrSet(groupResourceName, "created_at"),
					checkGroupParity(server),
				),
			},
			{
				// Applying the same configuration twice changes nothing.
				Config: providerConfig(server.URL()) + `
resource "immuta_group" "test" {
  name        = "Engineering"
  description = "Engineering department"
  email       = "engineering@immuta.com"
}
`,
				PlanOnly: true,
			},
			{
				ResourceName:      groupResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Renaming a group updates it in place.
				Config: providerConfig(server.URL()) + `
resource "immuta_group" "test" {
  name        = "Platform Engineering"
  description = "Platform team"
  email       = "engineering@immuta.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(groupResourceName, "name", "Platform Engineering"),
					resource.TestCheckResourceAttr(groupResourceName, "description", "Platform team"),
					checkGroupCount(server, 1),
					checkGroupParity(server),
				),
			},
			{
				// Removing an optional attribute clears it.
				Config: providerConfig(server.URL()) + `
resource "immuta_group" "test" {
  name = "Platform Engineering"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(groupResourceName, "description"),
					checkGroupParity(server),
				),
			},
		},
	})
}

func TestGroupResourceDetectsRemoteDrift(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + `
resource "immuta_group" "test" {
  name        = "Engineering"
  description = "Engineering department"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGroupsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					server.SetGroupDescription(1, "Changed in the Immuta UI")
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(groupResourceName, "description", "Engineering department"),
					checkGroupParity(server),
				),
			},
		},
	})
}

func TestGroupResourceHandlesRemoteDeletion(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + `
resource "immuta_group" "test" {
  name = "Engineering"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGroupsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					server.RemoveGroup(1)
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[groupResourceName]; ok {
						return errors.New("the deleted group should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check:  checkGroupCount(server, 1),
			},
		},
	})
}

func TestGroupResourceSurfacesAPIErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_group" "first" {
  name = "Engineering"
}

resource "immuta_group" "second" {
  name       = "Engineering"
  depends_on = [immuta_group.first]
}
`,
				ExpectError: regexp.MustCompile(`Unable to create the Immuta group`),
			},
		},
	})
}

func checkGroupParity(server *testutil.BIMServer) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[groupResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", groupResourceName)
		}

		client, err := immutaapi.NewClient(server.URL(), testutil.TestAPIKey)
		if err != nil {
			return fmt.Errorf("building an API client: %w", err)
		}

		id, err := parseTestInt64(resourceState.Primary.ID)
		if err != nil {
			return err
		}

		group, err := client.GetGroup(context.Background(), id)
		if err != nil {
			return fmt.Errorf("reading the group from the API: %w", err)
		}

		return testutil.GroupParityError(group, resourceState.Primary.Attributes)
	}
}

func checkGroupCount(server *testutil.BIMServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.GroupCount(); got != want {
			return fmt.Errorf("the API holds %d groups, want %d", got, want)
		}

		return nil
	}
}

func checkAllGroupsDestroyed(server *testutil.BIMServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.GroupCount(); got != 0 {
			return fmt.Errorf("%d groups are left in the API after destroy", got)
		}

		return nil
	}
}
