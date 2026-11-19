package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// Environment variables for TestAccConnectionResource. A connection needs a
// real data platform that Immuta can reach, so the test is opt-in on top of
// the usual acceptance test variables.
const (
	envTrinoHost     = "IMMUTA_TEST_TRINO_HOST"
	envTrinoUsername = "IMMUTA_TEST_TRINO_USERNAME"
	envTrinoPassword = "IMMUTA_TEST_TRINO_PASSWORD"
)

// TestAccConnectionResource registers, updates and deletes a Trino
// connection on a real Immuta tenant. Besides TF_ACC, IMMUTA_HOST,
// IMMUTA_API_KEY and IMMUTA_TEST_DOMAIN_NAME_PREFIX, it needs
// IMMUTA_TEST_TRINO_HOST, IMMUTA_TEST_TRINO_USERNAME and
// IMMUTA_TEST_TRINO_PASSWORD, and an API key with APPLICATION_ADMIN.
//
// Data object sync is disabled, and deleting the connection only deletes the
// data objects it registered. Run it against a sandbox tenant, never
// production.
func TestAccConnectionResource(t *testing.T) {
	for _, name := range []string{envTrinoHost, envTrinoUsername, envTrinoPassword} {
		if os.Getenv("TF_ACC") != "" && strings.TrimSpace(os.Getenv(name)) == "" {
			t.Skipf("set %s, %s and %s to run the connection acceptance test", envTrinoHost, envTrinoUsername, envTrinoPassword)
		}
	}

	key := acctest.RandomWithPrefix(accNamePrefix(t))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutil.RequireAccEnv(t) },
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             testAccCheckConnectionsDestroyed(t),
		Steps: []resource.TestStep{
			{
				Config: testAccConnectionConfig(key, "EVERY_24_HOURS"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_connection.test", "id", key),
					testAccCheckConnectionParity(t),
				),
			},
			{
				Config:   testAccConnectionConfig(key, "EVERY_24_HOURS"),
				PlanOnly: true,
			},
			{
				ResourceName:            "immuta_connection.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"configuration", "credential", "state"},
			},
			{
				Config: testAccConnectionConfig(key, "EVERY_12_HOURS"),
				Check:  testAccCheckConnectionParity(t),
			},
			{
				Config:   testAccConnectionConfig(key, "EVERY_12_HOURS"),
				PlanOnly: true,
			},
		},
	})
}

func testAccConnectionConfig(key, schedule string) string {
	return fmt.Sprintf(`
resource "immuta_connection" "test" {
  connection_key = %[1]q
  technology     = "Trino"

  configuration = <<-EOT
    clientConfiguration:
      host: %[2]s
      port: 443
      ssl: true
    dataObjectSync:
      enabled: false
      schedule: %[3]s
    syncOnCreate: false
  EOT

  credential = jsonencode({
    authenticationType = "UsernamePassword"
    username           = %[4]q
    password           = %[5]q
  })
}
`, key, os.Getenv(envTrinoHost), schedule, os.Getenv(envTrinoUsername), os.Getenv(envTrinoPassword))
}

// testAccCheckConnectionParity reads the connection from the Immuta API and
// checks that every configured setting matches.
func testAccCheckConnectionParity(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources["immuta_connection.test"]
		if !ok {
			return fmt.Errorf("immuta_connection.test is not in state")
		}

		connection, err := testutil.AccClient(t).GetConnection(context.Background(), resourceState.Primary.ID)
		if err != nil {
			return fmt.Errorf("reading connection %s from Immuta: %w", resourceState.Primary.ID, err)
		}

		configured, err := parseYAMLMapping(resourceState.Primary.Attributes["configuration"], "configuration")
		if err != nil {
			return err
		}
		delete(configured, "syncOnCreate")

		projected := projectOntoRemote(configured, remoteConnectionConfiguration(connection.Raw))
		if !equalJSON(projected, configured) {
			return fmt.Errorf("Terraform state and the Immuta API disagree:\n  api:   %v\n  state: %v", projected, configured)
		}

		return nil
	}
}

// testAccCheckConnectionsDestroyed asserts that every managed connection is
// gone from the tenant once the test has finished.
func testAccCheckConnectionsDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		client := testutil.AccClient(t)

		for name, resourceState := range state.RootModule().Resources {
			if resourceState.Type != "immuta_connection" {
				continue
			}

			_, err := client.GetConnection(context.Background(), resourceState.Primary.ID)
			switch {
			case err == nil:
				return fmt.Errorf("%s (%s) still exists in Immuta after destroy", name, resourceState.Primary.ID)
			case !immutaapi.IsNotFound(err):
				return fmt.Errorf("reading connection %s from Immuta: %w", resourceState.Primary.ID, err)
			}
		}

		return nil
	}
}
