package provider

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const userResourceName = "immuta_user.test"

func TestApplyUser(t *testing.T) {
	t.Parallel()

	t.Run("a fully populated user is copied onto the model", func(t *testing.T) {
		t.Parallel()

		model := userResourceModel{}
		profileID := int64(18)

		diags := model.applyUser(context.Background(), &immutaapi.User{
			ID:          18,
			IAMID:       "bim",
			UserID:      "jane.doe@immuta.com",
			Permissions: []string{"CREATE_PROJECT"},
			Disabled:    true,
			Profile: &immutaapi.UserProfile{
				ID:    &profileID,
				Name:  "Jane Doe",
				Email: "jane.doe@immuta.com",
			},
			CreatedAt: "2024-01-01T00:00:00.000Z",
		})
		if diags.HasError() {
			t.Fatalf("applyUser produced errors: %v", diags)
		}

		if model.ID != types.StringValue("18") {
			t.Errorf("id = %v, want 18", model.ID)
		}
		if model.IAMID != types.StringValue("bim") {
			t.Errorf("iam_id = %v, want bim", model.IAMID)
		}
		if model.Name != types.StringValue("Jane Doe") {
			t.Errorf("name = %v, want Jane Doe", model.Name)
		}
		if model.Disabled != types.BoolValue(true) {
			t.Errorf("disabled = %v, want true", model.Disabled)
		}
		if model.Phone != types.StringNull() {
			t.Errorf("phone = %v, want null", model.Phone)
		}
	})

	t.Run("a user without a profile falls back to the built-in identity manager", func(t *testing.T) {
		t.Parallel()

		model := userResourceModel{}

		diags := model.applyUser(context.Background(), &immutaapi.User{ID: 3, UserID: "jane.doe@immuta.com"})
		if diags.HasError() {
			t.Fatalf("applyUser produced errors: %v", diags)
		}

		if model.IAMID != types.StringValue(immutaapi.IAMIDBIM) {
			t.Errorf("iam_id = %v, want %q", model.IAMID, immutaapi.IAMIDBIM)
		}
		if model.Permissions.IsNull() {
			t.Error("permissions = null, want an empty set so that it matches the schema default")
		}
	})
}

func TestUserResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllUsersDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id     = "jane.doe@immuta.com"
  name        = "Jane Doe"
  email       = "jane.doe@immuta.com"
  location    = "Sydney"
  permissions = ["CREATE_PROJECT"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(userResourceName, "id"),
					resource.TestCheckResourceAttr(userResourceName, "iam_id", "bim"),
					resource.TestCheckResourceAttr(userResourceName, "user_id", "jane.doe@immuta.com"),
					resource.TestCheckResourceAttr(userResourceName, "name", "Jane Doe"),
					resource.TestCheckResourceAttr(userResourceName, "location", "Sydney"),
					resource.TestCheckResourceAttr(userResourceName, "disabled", "false"),
					resource.TestCheckResourceAttr(userResourceName, "permissions.#", "1"),
					resource.TestCheckTypeSetElemAttr(userResourceName, "permissions.*", "CREATE_PROJECT"),
					resource.TestCheckResourceAttrSet(userResourceName, "created_at"),
					checkUserParity(server),
				),
			},
			{
				// Applying the same configuration twice changes nothing.
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id     = "jane.doe@immuta.com"
  name        = "Jane Doe"
  email       = "jane.doe@immuta.com"
  location    = "Sydney"
  permissions = ["CREATE_PROJECT"]
}
`,
				PlanOnly: true,
			},
			{
				ResourceName:      userResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Changing the profile, permissions and disabled flag reuses
				// the existing user rather than replacing it.
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id      = "jane.doe@immuta.com"
  name         = "Jane Doe"
  email        = "jane.doe@immuta.com"
  location     = "Melbourne"
  organization = "Immuta"
  disabled     = true
  permissions  = ["CREATE_PROJECT", "USER_ADMIN"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(userResourceName, "location", "Melbourne"),
					resource.TestCheckResourceAttr(userResourceName, "organization", "Immuta"),
					resource.TestCheckResourceAttr(userResourceName, "disabled", "true"),
					resource.TestCheckResourceAttr(userResourceName, "permissions.#", "2"),
					checkUserCount(server, 1),
					checkUserParity(server),
				),
			},
		},
	})
}

func TestUserResourceReplacesTheUserWhenTheUsernameChanges(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllUsersDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id = "jane.doe@immuta.com"
}
`,
			},
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id = "jane.roe@immuta.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(userResourceName, "user_id", "jane.roe@immuta.com"),
					// The old user is deleted rather than left behind.
					checkUserCount(server, 1),
				),
			},
		},
	})
}

func TestUserResourceDetectsRemoteDrift(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id  = "jane.doe@immuta.com"
  location = "Sydney"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllUsersDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// A change made outside Terraform must show up as a diff.
				PreConfig: func() {
					server.SetUserProfileField("jane.doe@immuta.com", "location", "Changed in the Immuta UI")
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying the same configuration repairs the drift.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(userResourceName, "location", "Sydney"),
					checkUserParity(server),
				),
			},
		},
	})
}

func TestUserResourceHandlesRemoteDeletion(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id = "jane.doe@immuta.com"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllUsersDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// A user deleted outside Terraform is removed from state and
				// planned for recreation.
				PreConfig: func() {
					server.RemoveUser("jane.doe@immuta.com")
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[userResourceName]; ok {
						return errors.New("the deleted user should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkUserCount(server, 1),
					checkUserParity(server),
				),
			},
		},
	})
}

func TestUserResourceSurfacesAPIErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// The fake API rejects a duplicate username with a conflict,
				// the same way Immuta does.
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "first" {
  user_id = "jane.doe@immuta.com"
}

resource "immuta_user" "second" {
  user_id    = "jane.doe@immuta.com"
  depends_on = [immuta_user.first]
}
`,
				ExpectError: regexp.MustCompile(`Unable to create the Immuta user`),
			},
		},
	})
}

func TestUserResourceRejectsInvalidConfiguration(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_user" "test" {
  user_id = ""
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Attribute user_id string length must be at least 1`),
			},
		},
	})
}

// checkUserParity asserts that Terraform state matches what the API stored.
func checkUserParity(server *testutil.BIMServer) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[userResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", userResourceName)
		}

		client, err := immutaapi.NewClient(server.URL(), testutil.TestAPIKey)
		if err != nil {
			return fmt.Errorf("building an API client: %w", err)
		}

		id, err := parseTestInt64(resourceState.Primary.ID)
		if err != nil {
			return err
		}

		user, err := client.GetUser(context.Background(), resourceState.Primary.Attributes["iam_id"], id)
		if err != nil {
			return fmt.Errorf("reading the user from the API: %w", err)
		}

		return testutil.UserParityError(user, resourceState.Primary.Attributes)
	}
}

func checkUserCount(server *testutil.BIMServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.UserCount(); got != want {
			return fmt.Errorf("the API holds %d users, want %d", got, want)
		}

		return nil
	}
}

// checkAllUsersDestroyed asserts that destroying the resource removed the
// user from Immuta rather than only disabling it.
func checkAllUsersDestroyed(server *testutil.BIMServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.UserCount(); got != 0 {
			return fmt.Errorf("%d users are left in the API after destroy", got)
		}

		return nil
	}
}

func parseTestInt64(value string) (int64, error) {
	var parsed int64
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return 0, fmt.Errorf("the resource ID %q is not numeric: %w", value, err)
	}

	return parsed, nil
}

// TestUserResourceHandlesTenantDefaultPermissions reproduces a tenant that
// grants CREATE_PROJECT to every new user. Configured permissions must win,
// including an explicitly empty set, and unset permissions must record what
// Immuta assigned instead of producing an inconsistent result.
func TestUserResourceHandlesTenantDefaultPermissions(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)
	server.SetDefaultPermissions("CREATE_PROJECT")

	config := providerConfig(server.URL()) + `
resource "immuta_user" "ada" {
  user_id     = "ada.lovelace@immuta.com"
  permissions = []
}

resource "immuta_user" "jane" {
  user_id     = "jane.doe@immuta.com"
  permissions = ["USER_ADMIN"]
}

resource "immuta_user" "alan" {
  user_id = "alan.turing@immuta.com"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllUsersDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					// An explicit empty set strips the tenant default.
					resource.TestCheckResourceAttr("immuta_user.ada", "permissions.#", "0"),
					checkStoredPermissions(server, "ada.lovelace@immuta.com", nil),
					// A configured set replaces the tenant default.
					resource.TestCheckResourceAttr("immuta_user.jane", "permissions.#", "1"),
					resource.TestCheckTypeSetElemAttr("immuta_user.jane", "permissions.*", "USER_ADMIN"),
					checkStoredPermissions(server, "jane.doe@immuta.com", []string{"USER_ADMIN"}),
					// Unset permissions are not managed and keep the default.
					resource.TestCheckResourceAttr("immuta_user.alan", "permissions.#", "1"),
					resource.TestCheckTypeSetElemAttr("immuta_user.alan", "permissions.*", "CREATE_PROJECT"),
					checkStoredPermissions(server, "alan.turing@immuta.com", []string{"CREATE_PROJECT"}),
				),
			},
			{
				// Nothing drifts on the next plan.
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

func checkStoredPermissions(server *testutil.BIMServer, userID string, want []string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		user, ok := server.User(userID)
		if !ok {
			return fmt.Errorf("the API does not hold the user %s", userID)
		}

		got := append([]string(nil), user.Permissions...)
		sort.Strings(got)
		wantSorted := append([]string(nil), want...)
		sort.Strings(wantSorted)

		if len(got) != len(wantSorted) || (len(got) > 0 && !reflect.DeepEqual(got, wantSorted)) {
			return fmt.Errorf("the API holds permissions %v for %s, want %v", got, userID, wantSorted)
		}

		return nil
	}
}
