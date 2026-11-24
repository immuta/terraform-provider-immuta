package provider

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// envGlobalUserUsername is the username of an existing user, looked up by
// TestAccGlobalUserDataSource.
const envGlobalUserUsername = "IMMUTA_TEST_GLOBAL_USER_USERNAME"

// TestAccGlobalUserDataSource looks up an existing user's global user ID on a
// real Immuta tenant. It only reads. It checks the provider's assumption that
// the Request app user search filters on usernames.
//
// Besides the usual acceptance test variables, it needs
// IMMUTA_REQUEST_APP_URL, IMMUTA_REQUEST_APP_TOKEN and
// IMMUTA_TEST_GLOBAL_USER_USERNAME.
func TestAccGlobalUserDataSource(t *testing.T) {
	required := []string{testutil.EnvRequestAppURL, testutil.EnvRequestAppToken, envGlobalUserUsername}
	for _, name := range required {
		if os.Getenv("TF_ACC") != "" && strings.TrimSpace(os.Getenv(name)) == "" {
			t.Skipf("set %s to run the global user acceptance test", strings.Join(required, ", "))
		}
	}

	username := os.Getenv(envGlobalUserUsername)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutil.RequireAccEnv(t) },
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
data "immuta_global_user" "test" {
  username = %q
}
`, username),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.immuta_global_user.test", "username", username),
				resource.TestMatchResourceAttr("data.immuta_global_user.test", "global_user_id",
					regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)),
				resource.TestCheckResourceAttrPair("data.immuta_global_user.test", "id", "data.immuta_global_user.test", "global_user_id"),
				resource.TestCheckResourceAttrSet("data.immuta_global_user.test", "iam_id"),
			),
		}},
	})
}
