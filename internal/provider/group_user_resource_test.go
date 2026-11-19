package provider

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const groupUserResourceName = "immuta_group_user.test"

// groupUserConfig builds a configuration with one user, one group and one
// membership between them.
func groupUserConfig(serverURL, username string) string {
	return providerConfig(serverURL) + fmt.Sprintf(`
resource "immuta_user" "test" {
  user_id = %q
}

resource "immuta_group" "test" {
  name = "Engineering"
}

resource "immuta_group_user" "test" {
  group_id = immuta_group.test.id
  user_id  = immuta_user.test.user_id
}
`, username)
}

func TestGroupUserResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := groupUserConfig(server.URL(), "jane.doe@immuta.com")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllMembershipsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(groupUserResourceName, "iam_id", "bim"),
					resource.TestCheckResourceAttr(groupUserResourceName, "user_id", "jane.doe@immuta.com"),
					resource.TestCheckResourceAttrPair(groupUserResourceName, "group_id", groupResourceName, "id"),
					resource.TestCheckResourceAttrSet(groupUserResourceName, "membership_id"),
					checkMembershipCount(server, 1),
				),
			},
			{
				// Applying the same configuration twice changes nothing.
				Config:   config,
				PlanOnly: true,
			},
			{
				// The resource ID doubles as the import ID.
				ResourceName:      groupUserResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestGroupUserResourceReplacesTheMembershipWhenTheUserChanges(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllMembershipsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: groupUserConfig(server.URL(), "jane.doe@immuta.com")},
			{
				Config: groupUserConfig(server.URL(), "jane.roe@immuta.com"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(groupUserResourceName, "user_id", "jane.roe@immuta.com"),
					checkMembershipCount(server, 1),
				),
			},
		},
	})
}

func TestGroupUserResourceHandlesRemoteRemoval(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := groupUserConfig(server.URL(), "jane.doe@immuta.com")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllMembershipsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// A membership removed in the Immuta UI is removed from state
				// and planned for recreation.
				PreConfig: func() {
					for id := int64(1); id <= 10; id++ {
						server.RemoveMembership(id)
					}
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[groupUserResourceName]; ok {
						return errors.New("the removed membership should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check:  checkMembershipCount(server, 1),
			},
		},
	})
}

func TestGroupUserResourceSurfacesAPIErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Immuta rejects a duplicate membership with a conflict.
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id = "jane.doe@immuta.com"
}

resource "immuta_group" "test" {
  name = "Engineering"
}

resource "immuta_group_user" "test" {
  group_id = immuta_group.test.id
  user_id  = immuta_user.test.user_id
}

resource "immuta_group_user" "duplicate" {
  group_id   = immuta_group.test.id
  user_id    = immuta_user.test.user_id
  depends_on = [immuta_group_user.test]
}
`,
				ExpectError: regexp.MustCompile(`Unable to add the Immuta user`),
			},
		},
	})
}

func TestSplitGroupUserID(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		id        string
		wantIAM   string
		wantUser  string
		wantGroup string
		wantOK    bool
	}{
		"a well formed id": {
			id: "bim/jane.doe@immuta.com/12", wantIAM: "bim", wantUser: "jane.doe@immuta.com", wantGroup: "12", wantOK: true,
		},
		"a username containing a slash": {
			id: "bim/domain/jane/12", wantIAM: "bim", wantUser: "domain/jane", wantGroup: "12", wantOK: true,
		},
		"too few segments":  {id: "bim/12"},
		"an empty id":       {id: ""},
		"an empty IAM id":   {id: "/jane/12"},
		"an empty group id": {id: "bim/jane/"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			iamID, userID, groupID, ok := splitGroupUserID(test.id)
			if ok != test.wantOK {
				t.Fatalf("ok = %t, want %t", ok, test.wantOK)
			}

			if !test.wantOK {
				return
			}

			if iamID != test.wantIAM || userID != test.wantUser || groupID != test.wantGroup {
				t.Errorf("= (%q, %q, %q), want (%q, %q, %q)", iamID, userID, groupID, test.wantIAM, test.wantUser, test.wantGroup)
			}
		})
	}
}

func checkMembershipCount(server *testutil.BIMServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.MembershipCount(); got != want {
			return fmt.Errorf("the API holds %d group memberships, want %d", got, want)
		}

		return nil
	}
}

func checkAllMembershipsDestroyed(server *testutil.BIMServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.MembershipCount(); got != 0 {
			return fmt.Errorf("%d group memberships are left in the API after destroy", got)
		}

		return nil
	}
}
