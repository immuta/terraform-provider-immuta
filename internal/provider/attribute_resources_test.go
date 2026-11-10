package provider

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const (
	userAttributeResourceName  = "immuta_user_attribute.test"
	groupAttributeResourceName = "immuta_group_attribute.test"
)

// attributeConfig builds a configuration with a user, a group and one
// attribute on each.
func attributeConfig(serverURL, value string) string {
	return providerConfig(serverURL) + fmt.Sprintf(`
resource "immuta_user" "test" {
  user_id = "jane.doe@immuta.com"
}

resource "immuta_group" "test" {
  name = "Engineering"
}

resource "immuta_user_attribute" "test" {
  user_id         = immuta_user.test.user_id
  user_profile_id = immuta_user.test.id
  name            = "Department"
  value           = %q
}

resource "immuta_group_attribute" "test" {
  group_id = immuta_group.test.id
  name     = "Department"
  value    = "Engineering"
}
`, value)
}

func TestAttributeResourcesLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := attributeConfig(server.URL(), "Finance")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(userAttributeResourceName, "iam_id", "bim"),
					resource.TestCheckResourceAttr(userAttributeResourceName, "name", "Department"),
					resource.TestCheckResourceAttr(userAttributeResourceName, "value", "Finance"),
					resource.TestCheckResourceAttr(groupAttributeResourceName, "value", "Engineering"),
					checkUserAttributes(server, "jane.doe@immuta.com", map[string][]string{"Department": {"Finance"}}),
				),
			},
			{
				// Applying the same configuration twice changes nothing.
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:      userAttributeResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      groupAttributeResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Changing the value replaces the assignment: the old value is
				// removed rather than left behind.
				Config: attributeConfig(server.URL(), "Legal"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(userAttributeResourceName, "value", "Legal"),
					checkUserAttributes(server, "jane.doe@immuta.com", map[string][]string{"Department": {"Legal"}}),
				),
			},
		},
	})
}

func TestUserAttributeResourceHandlesRemoteRemoval(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := attributeConfig(server.URL(), "Finance")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// An attribute removed in the Immuta UI is removed from state
				// and planned for reassignment.
				PreConfig: func() {
					server.RemoveUserAttribute("jane.doe@immuta.com", "Department", "Finance")
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[userAttributeResourceName]; ok {
						return errors.New("the removed attribute should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check:  checkUserAttributes(server, "jane.doe@immuta.com", map[string][]string{"Department": {"Finance"}}),
			},
		},
	})
}

func TestUserAttributeResourceAssignsSeveralValues(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Separate resources hold separate values of the same
				// attribute, and neither clobbers the other.
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id = "jane.doe@immuta.com"
}

resource "immuta_user_attribute" "finance" {
  user_id         = immuta_user.test.user_id
  user_profile_id = immuta_user.test.id
  name            = "Department"
  value           = "Finance"
}

resource "immuta_user_attribute" "legal" {
  user_id         = immuta_user.test.user_id
  user_profile_id = immuta_user.test.id
  name            = "Department"
  value           = "Legal"
}
`,
				Check: checkUserAttributes(server, "jane.doe@immuta.com", map[string][]string{"Department": {"Finance", "Legal"}}),
			},
		},
	})
}

func TestUserHoldsAttribute(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		user *immutaapi.User
		want bool
	}{
		"assigned to the BIM profile": {
			user: &immutaapi.User{BIMAuthorizations: immutaapi.Authorizations{"Department": {"Finance"}}},
			want: true,
		},
		"inherited from a group only": {
			// bimAuthorizations is present but empty, so the attribute in the
			// combined view came from a group and is not owned here.
			user: &immutaapi.User{
				BIMAuthorizations: immutaapi.Authorizations{},
				Authorizations:    immutaapi.Authorizations{"Department": {"Finance"}},
			},
			want: false,
		},
		"falls back to the combined view when bimAuthorizations is absent": {
			user: &immutaapi.User{Authorizations: immutaapi.Authorizations{"Department": {"Finance"}}},
			want: true,
		},
		"not assigned at all": {
			user: &immutaapi.User{BIMAuthorizations: immutaapi.Authorizations{"Country": {"AU"}}},
			want: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := userHoldsAttribute(test.user, "Department", "Finance"); got != test.want {
				t.Errorf("userHoldsAttribute = %t, want %t", got, test.want)
			}
		})
	}
}

func checkUserAttributes(server *testutil.BIMServer, userID string, want map[string][]string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		// Terraform applies independent resources in parallel, so the order
		// Immuta records the values in is not deterministic.
		got := server.UserAttributes(userID)
		for _, values := range got {
			sort.Strings(values)
		}

		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("the API holds attributes %v for %s, want %v", got, userID, want)
		}

		return nil
	}
}
