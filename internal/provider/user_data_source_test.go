package provider

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

func TestUserDataSourceLookups(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	janeID := strconv.FormatInt(server.AddUser("okta", "jane", "Jane Doe", "Jane.Doe@example.com"), 10)
	// A partial match for every lookup below, which must not be picked.
	server.AddUser("okta", "jane2", "Jane Doe II", "jane.doe@example.com.au")
	// The same person in two identity managers.
	server.AddUser("okta", "sam", "Sam Smith", "sam@example.com")
	samBIMID := strconv.FormatInt(server.AddUser("bim", "sam.smith", "Sam Smith", "sam@example.com"), 10)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
data "immuta_user" "by_email" {
  email = "jane.doe@EXAMPLE.com"
}

data "immuta_user" "by_user_id" {
  user_id = "jane"
}

data "immuta_user" "in_iam" {
  email  = "sam@example.com"
  iam_id = "bim"
}

# Users Terraform does not manage can be given attributes.
resource "immuta_user_attribute" "jane_department" {
  iam_id          = data.immuta_user.by_email.iam_id
  user_id         = data.immuta_user.by_email.user_id
  user_profile_id = data.immuta_user.by_email.id
  name            = "Department"
  value           = "Finance"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_user.by_email", "id", janeID),
					resource.TestCheckResourceAttr("data.immuta_user.by_email", "iam_id", "okta"),
					resource.TestCheckResourceAttr("data.immuta_user.by_email", "user_id", "jane"),
					// The email as Immuta holds it, not as configured.
					resource.TestCheckResourceAttr("data.immuta_user.by_email", "email", "Jane.Doe@example.com"),
					resource.TestCheckResourceAttr("data.immuta_user.by_email", "name", "Jane Doe"),
					resource.TestCheckResourceAttr("data.immuta_user.by_email", "disabled", "false"),
					resource.TestCheckResourceAttr("data.immuta_user.by_user_id", "id", janeID),
					resource.TestCheckResourceAttr("data.immuta_user.by_user_id", "email", "Jane.Doe@example.com"),
					resource.TestCheckResourceAttr("data.immuta_user.in_iam", "id", samBIMID),
					resource.TestCheckResourceAttr("data.immuta_user.in_iam", "user_id", "sam.smith"),
					resource.TestCheckResourceAttr("immuta_user_attribute.jane_department", "user_profile_id", janeID),
					func(*terraform.State) error {
						if got := server.UserAttributes("jane")["Department"]; len(got) != 1 || got[0] != "Finance" {
							return fmt.Errorf("Department of jane = %v, want [Finance]", got)
						}

						return nil
					},
				),
			},
		},
	})
}

func TestUserDataSourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewBIMServer()
	t.Cleanup(server.Close)

	server.AddUser("okta", "sam", "Sam Smith", "sam@example.com")
	server.AddUser("bim", "sam.smith", "Sam Smith", "sam@example.com")
	server.AddUser("okta", "jane", "Jane Doe", "jane@example.com")

	cases := map[string]struct {
		config string
		want   string
	}{
		"no lookup": {
			`data "immuta_user" "test" {
  iam_id = "bim"
}`,
			`(?s)No attribute specified.*email.*user_id`,
		},
		"both lookups": {
			`data "immuta_user" "test" {
  email   = "sam@example.com"
  user_id = "sam"
}`,
			`Invalid Attribute Combination`,
		},
		"only a partial match": {
			`data "immuta_user" "test" {
  email = "jane@example"
}`,
			`(?s)No\s+enabled\s+Immuta\s+user\s+has\s+email\s+"jane@example".*exact`,
		},
		"wrong identity manager": {
			`data "immuta_user" "test" {
  user_id = "jane"
  iam_id  = "bim"
}`,
			`No\s+enabled\s+Immuta\s+user\s+has\s+user_id\s+"jane"\s+in\s+iam_id\s+"bim"`,
		},
		"several identity managers": {
			`data "immuta_user" "test" {
  email = "sam@example.com"
}`,
			`(?s)2\s+Immuta\s+users\s+have\s+email.*iam_id\s+"bim".*iam_id\s+"okta".*Set\s+iam_id`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{Config: providerConfig(server.URL()) + tc.config, ExpectError: regexp.MustCompile(tc.want)},
				},
			})
		})
	}
}

func TestExactUserMatches(t *testing.T) {
	t.Parallel()

	user := func(id int64, iamID, userID, email string) immutaapi.User {
		return immutaapi.User{ID: id, IAMID: iamID, UserID: userID, Profile: &immutaapi.UserProfile{Email: email}}
	}
	users := []immutaapi.User{
		user(1, "okta", "jane", "Jane@Example.com"),
		user(2, "okta", "jane2", "jane@example.com.au"),
		user(3, "bim", "jane", "jane@example.com"),
		{IAMID: "okta", UserID: "jane"}, // no profile id: unusable
	}

	ids := func(matches []immutaapi.User) []int64 {
		var got []int64
		for _, match := range matches {
			got = append(got, match.ID)
		}

		return got
	}

	cases := map[string]struct {
		opts immutaapi.SearchUsersOptions
		want []int64
	}{
		"email ignores case": {immutaapi.SearchUsersOptions{Email: "jane@example.com"}, []int64{1, 3}},
		"email in iam":       {immutaapi.SearchUsersOptions{Email: "jane@example.com", IAMID: "bim"}, []int64{3}},
		"user id is exact":   {immutaapi.SearchUsersOptions{UserID: "jane", IAMID: "okta"}, []int64{1}},
		"user id is cased":   {immutaapi.SearchUsersOptions{UserID: "Jane"}, nil},
	}
	for name, tc := range cases {
		if got := ids(exactUserMatches(users, tc.opts)); !slicesEqual(got, tc.want) {
			t.Errorf("%s: matches = %v, want %v", name, got, tc.want)
		}
	}
	if len(users) != 4 {
		t.Error("exactUserMatches changed its input")
	}
}

func slicesEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
