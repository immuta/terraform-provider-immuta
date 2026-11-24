package provider

import (
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

func TestGlobalUserDataSourceLookups(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	// Partial matches for the lookups below, which must not be picked.
	server.AddUser("okta", "jane2", "Jane Doe II", "jane2@example.com")
	server.AddUser("okta", "mary", "Mary Jane", "")
	janeID := server.AddUser("okta", "jane", "Jane Doe", "jane@example.com")
	// The same username in two identity managers, one without an email.
	server.AddUser("okta", "sam", "Sam Smith", "sam@example.com")
	samBIMID := server.AddUser("bim", "sam", "Sam Smith", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{{
			Config: dataProductProviderConfig(server) + fmt.Sprintf(`
data "immuta_global_user" "jane" {
  username = "jane"
}

data "immuta_global_user" "sam" {
  username = "sam"
  iam_id   = "bim"
}

# The reason for the lookup: a data product's subject matter expert.
resource "immuta_data_product" "test" {
  name                     = "Customer 360"
  domains                  = [%q]
  subject_matter_expert_id = data.immuta_global_user.jane.global_user_id
  data_sources             = ["1"]

  data_access_request_form_id       = %q
  masking_exception_request_form_id = %q
}
`, testDataProductDomain, testDataProductRequestForm, testDataProductRequestForm),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.immuta_global_user.jane", "id", janeID),
				resource.TestCheckResourceAttr("data.immuta_global_user.jane", "global_user_id", janeID),
				resource.TestCheckResourceAttr("data.immuta_global_user.jane", "iam_id", "okta"),
				resource.TestCheckResourceAttr("data.immuta_global_user.jane", "name", "Jane Doe"),
				resource.TestCheckResourceAttr("data.immuta_global_user.jane", "email", "jane@example.com"),
				resource.TestCheckResourceAttr("data.immuta_global_user.sam", "global_user_id", samBIMID),
				resource.TestCheckResourceAttr("data.immuta_global_user.sam", "email", ""),
				resource.TestCheckResourceAttr("immuta_data_product.test", "subject_matter_expert_id", janeID),
				func(state *terraform.State) error {
					id := state.RootModule().Resources["immuta_data_product.test"].Primary.ID
					definition, _, ok := server.Product(id)
					if !ok {
						return fmt.Errorf("data product %s was not created", id)
					}
					expert := definition["metadata"].(map[string]any)["subjectMatterExpert"].(map[string]any)["id"]
					if expert != janeID {
						return fmt.Errorf("subject matter expert sent to Immuta = %v, want %s", expert, janeID)
					}

					return nil
				},
			),
		}},
	})
}

func TestGlobalUserDataSourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	server.AddUser("okta", "sam", "Sam Smith", "sam@example.com")
	server.AddUser("bim", "sam", "Sam Smith", "sam@example.com")
	server.AddUser("okta", "jane", "Jane Doe", "jane@example.com")
	for i := range immutaapi.RequestAppUserSearchLimit {
		server.AddUser("okta", fmt.Sprintf("analyst%03d", i), "Analyst", "")
	}
	// Beyond the search limit for "analyst".
	server.AddUser("okta", "analyst", "Analyst", "")

	cases := map[string]struct {
		provider, config, want string
	}{
		"no username": {
			config: `data "immuta_global_user" "test" {}`,
			want:   `(?s)Missing required argument.*username`,
		},
		"only a partial match": {
			config: `data "immuta_global_user" "test" {
  username = "jan"
}`,
			want: `(?s)No\s+Immuta\s+user\s+has\s+username\s+"jan".*exact`,
		},
		"usernames are matched exactly": {
			config: `data "immuta_global_user" "test" {
  username = "Jane"
}`,
			want: `No\s+Immuta\s+user\s+has\s+username\s+"Jane"`,
		},
		"wrong identity manager": {
			config: `data "immuta_global_user" "test" {
  username = "jane"
  iam_id   = "bim"
}`,
			want: `No\s+Immuta\s+user\s+has\s+username\s+"jane"\s+in\s+iam_id\s+"bim"`,
		},
		"several identity managers": {
			config: `data "immuta_global_user" "test" {
  username = "sam"
}`,
			want: `(?s)2\s+Immuta\s+users\s+have\s+username\s+"sam".*iam_id\s+"bim".*iam_id\s+"okta".*Set\s+iam_id`,
		},
		"search truncated": {
			config: `data "immuta_global_user" "test" {
  username = "analyst"
}`,
			want: `(?s)No\s+Immuta\s+user\s+has\s+username\s+"analyst".*maximum\s+of\s+100\s+users`,
		},
		"request app not configured": {
			provider: providerConfig(server.URL()),
			config: `data "immuta_global_user" "test" {
  username = "jane"
}`,
			want: `(?s)Immuta\s+Request\s+app\s+API\s+not\s+configured.*immuta_global_user`,
		},
		"govern api key as the token": {
			provider: fmt.Sprintf(`
provider "immuta" {
  host              = %q
  api_key           = %q
  request_app_url   = %q
  request_app_token = %q
}
`, server.URL(), testutil.TestAPIKey, server.RequestAppURL(), testutil.TestAPIKey),
			config: `data "immuta_global_user" "test" {
  username = "jane"
}`,
			want: `did\s+not\s+accept\s+the\s+personal\s+access\s+token`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider := tc.provider
			if provider == "" {
				provider = dataProductProviderConfig(server)
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{Config: provider + tc.config, ExpectError: regexp.MustCompile(tc.want)},
				},
			})
		})
	}
}

func TestExactGlobalUserMatches(t *testing.T) {
	t.Parallel()

	users := []immutaapi.RequestAppUser{
		{IAMID: "okta", GlobalUserID: "g1", Username: "jane"},
		{IAMID: "okta", GlobalUserID: "g2", Username: "jane2"},
		{IAMID: "bim", GlobalUserID: "g3", Username: "jane"},
		{IAMID: "okta", Username: "jane"}, // no global user id: unusable
	}

	ids := func(matches []immutaapi.RequestAppUser) []string {
		var got []string
		for _, match := range matches {
			got = append(got, match.GlobalUserID)
		}

		return got
	}

	cases := map[string]struct {
		username, iamID string
		want            []string
	}{
		"any identity manager": {"jane", "", []string{"g1", "g3"}},
		"in an iam":            {"jane", "bim", []string{"g3"}},
		"case matters":         {"Jane", "", nil},
	}
	for name, tc := range cases {
		if got := ids(exactGlobalUserMatches(users, tc.username, tc.iamID)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: matches = %v, want %v", name, got, tc.want)
		}
	}
	if len(users) != 4 {
		t.Error("exactGlobalUserMatches changed its input")
	}
}
