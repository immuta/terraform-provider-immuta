package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// fastConnectionPolling shortens the waits for asynchronous connection
// operations. Tests using it must not run in parallel.
func fastConnectionPolling(t *testing.T) {
	t.Helper()

	interval, settle, deletion := connectionPollInterval, connectionSettleTimeout, connectionDeleteTimeout
	connectionPollInterval, connectionSettleTimeout, connectionDeleteTimeout = 10*time.Millisecond, 5*time.Second, 5*time.Second
	t.Cleanup(func() {
		connectionPollInterval, connectionSettleTimeout, connectionDeleteTimeout = interval, settle, deletion
	})
}

func exampleConnectionFile(t *testing.T, name string) string {
	t.Helper()

	file, err := filepath.Abs(filepath.Join("..", "..", "examples", "resources", "immuta_connection", "connections", name+".yaml.tftpl"))
	if err != nil {
		t.Fatal(err)
	}

	return file
}

// connectionConfig is one connection whose configuration is read from a file.
func connectionConfig(name, key, technology, file, credential string) string {
	return fmt.Sprintf(`
resource "immuta_connection" %q {
  connection_key = %q
  technology     = %q
  configuration  = file(%q)
  credential     = jsonencode(%s)
}
`, name, key, technology, file, credential)
}

// exampleConnectionOwnerID is the owner profile ID the example templates are
// rendered with, as data.immuta_user would provide it: a string.
const exampleConnectionOwnerID = "2"

// exampleConnectionConfig is one connection whose configuration is rendered
// from an example template, as the examples do.
func exampleConnectionConfig(name, key, technology, file, credential string) string {
	return fmt.Sprintf(`
resource "immuta_connection" %q {
  connection_key = %q
  technology     = %q
  configuration  = templatefile(%q, { owner_id = %q })
  credential     = jsonencode(%s)
}
`, name, key, technology, file, exampleConnectionOwnerID, credential)
}

// renderExampleConnection renders an example template the way templatefile
// does for the only variable the examples use.
func renderExampleConnection(t *testing.T, name string) (map[string]any, error) {
	t.Helper()

	content, err := os.ReadFile(exampleConnectionFile(t, name))
	if err != nil {
		return nil, err
	}

	return parseYAMLMapping(strings.ReplaceAll(string(content), "${owner_id}", exampleConnectionOwnerID), "configuration")
}

const (
	trinoCredential      = `{ authenticationType = "UsernamePassword", username = "svc", password = "first" }`
	databricksCredential = `{ authenticationType = "Token", token = "dapi-secret" }`
	snowflakeCredential  = `{ authenticationType = "OAuthWithSecret", clientId = "c", authorityUrl = "https://login.example.com", scope = "s", clientSecret = "sec" }`
)

func TestConnectionResourceFromExampleFiles(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) +
		exampleConnectionConfig("trino", "trino", "Trino", exampleConnectionFile(t, "trino"), trinoCredential) +
		exampleConnectionConfig("databricks", "databricks", "Databricks", exampleConnectionFile(t, "databricks"), databricksCredential) +
		exampleConnectionConfig("snowflake", "snowflake", "Snowflake", exampleConnectionFile(t, "snowflake"), snowflakeCredential)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllConnectionsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_connection.trino", "id", "trino"),
					// The provider waited for Immuta to finish activating.
					resource.TestCheckResourceAttr("immuta_connection.trino", "state", "ACTIVE"),
					resource.TestCheckResourceAttr("immuta_connection.snowflake", "state", "ACTIVE"),
					checkConnectionRegisteredFromFile(t, server, "trino", "Trino", "trino",
						map[string]any{"authenticationType": "UsernamePassword", "username": "svc", "password": "first"}),
					checkConnectionRegisteredFromFile(t, server, "databricks", "Databricks", "databricks",
						map[string]any{"authenticationType": "Token", "token": "dapi-secret"}),
					checkConnectionRegisteredFromFile(t, server, "snowflake", "Snowflake", "snowflake",
						map[string]any{"authenticationType": "OAuthWithSecret", "clientId": "c", "authorityUrl": "https://login.example.com", "scope": "s", "clientSecret": "sec"}),
				),
			},
			{
				// Defaults and fields Immuta adds, the redacted credential, the
				// create-only syncOnCreate and the templated owner are not drift.
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:      "immuta_connection.trino",
				ImportState:       true,
				ImportStateVerify: true,
				// Secrets cannot be read back, and an import starts from
				// everything Immuta returns.
				ImportStateVerifyIgnore: []string{"configuration", "credential"},
			},
		},
	})
}

// checkConnectionRegisteredFromFile asserts that Immuta received the rendered
// YAML template, the attributes and the credential as one registration
// payload, with the owner profile ID as a number.
func checkConnectionRegisteredFromFile(t *testing.T, server *testutil.ConnectionServer, key, technology, file string, credential map[string]any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		configuration, err := renderExampleConnection(t, file)
		if err != nil {
			return err
		}
		owners := configuration["dataObjectDefaults"].(map[string]any)["owners"].([]any)
		if !equalJSON(owners, []any{map[string]any{"type": "USER", "id": 2}}) {
			return fmt.Errorf("owners of %s = %v, want the profile ID as a number", file, owners)
		}

		body, ok := server.Body(key)
		if !ok {
			return fmt.Errorf("connection %s was not registered", key)
		}
		if want := connectionCreateBody(key, technology, configuration, credential); !equalJSON(body, want) {
			return fmt.Errorf("connection %s was registered with %v, want %v", key, body, want)
		}

		return nil
	}
}

const trinoConfigurationYAML = `clientConfiguration:
  host: trino.example.com
  port: 443
  ssl: true
dataObjectSync:
  enabled: true
  schedule: EVERY_24_HOURS
syncOnCreate: true
`

func TestConnectionResourceUpdatesOnlyWhatChanged(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	file := filepath.Join(t.TempDir(), "trino.yaml")
	write := func(content string) func() {
		return func() {
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(trinoConfigurationYAML)()

	config := func(password string) string {
		return providerConfig(server.URL()) + connectionConfig("test", "trino", "Trino", file,
			fmt.Sprintf(`{ authenticationType = "UsernamePassword", username = "svc", password = %q }`, password))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllConnectionsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config("first")},
			{
				// One setting changed: only it is sent, without the credential.
				PreConfig: write(strings.Replace(trinoConfigurationYAML, "EVERY_24_HOURS", "EVERY_6_HOURS", 1)),
				Config:    config("first"),
				Check: checkLastConnectionPatch(server, 1, map[string]any{
					"dataObjectSync": map[string]any{"schedule": "EVERY_6_HOURS"},
				}),
			},
			{
				// A new credential is sent on its own.
				Config: config("second"),
				Check: checkLastConnectionPatch(server, 2, map[string]any{
					"clientConfiguration": map[string]any{"credential": map[string]any{
						"authenticationType": "UsernamePassword", "username": "svc", "password": "second",
					}},
				}),
			},
			{
				// Trino's host can change in place.
				PreConfig: write(strings.NewReplacer("EVERY_24_HOURS", "EVERY_6_HOURS", "trino.example.com", "trino2.example.com").Replace(trinoConfigurationYAML)),
				Config:    config("second"),
				Check: checkLastConnectionPatch(server, 3, map[string]any{
					"clientConfiguration": map[string]any{"host": "trino2.example.com"},
				}),
			},
			{
				// syncOnCreate only applies at registration: nothing is sent.
				PreConfig: write(strings.NewReplacer("EVERY_24_HOURS", "EVERY_6_HOURS", "trino.example.com", "trino2.example.com",
					"syncOnCreate: true", "syncOnCreate: false").Replace(trinoConfigurationYAML)),
				Config: config("second"),
				Check:  checkLastConnectionPatch(server, 3, nil),
			},
			{
				Config:   config("second"),
				PlanOnly: true,
			},
			{
				Config: config("second"),
				Check: func(*terraform.State) error {
					if server.Creates() != 1 || server.Deletes() != 0 {
						return fmt.Errorf("creates = %d, deletes = %d: every change should have been in place", server.Creates(), server.Deletes())
					}

					return nil
				},
			},
		},
	})
}

func TestConnectionResourceReplacesForFixedSettings(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	file := filepath.Join(t.TempDir(), "snowflake.yaml")
	snowflake := "clientConfiguration:\n  host: one.snowflakecomputing.com\n  warehouse: COMPUTE\n"
	if err := os.WriteFile(file, []byte(snowflake), 0o600); err != nil {
		t.Fatal(err)
	}
	config := providerConfig(server.URL()) + connectionConfig("test", "snowflake", "Snowflake", file, snowflakeCredential)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllConnectionsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// Snowflake's host cannot be updated, so the connection is
				// deleted and registered again under the same key.
				PreConfig: func() {
					if err := os.WriteFile(file, []byte(strings.Replace(snowflake, "one.", "two.", 1)), 0o600); err != nil {
						t.Fatal(err)
					}
				},
				Config: config,
				Check: func(*terraform.State) error {
					body, _ := server.Body("snowflake")
					host := body["clientConfiguration"].(map[string]any)["host"]
					switch {
					case server.Deletes() != 1 || server.Creates() != 2:
						return fmt.Errorf("creates = %d, deletes = %d, want a replacement", server.Creates(), server.Deletes())
					case len(server.Patches()) != 0:
						return fmt.Errorf("patches = %v, want none", server.Patches())
					case host != "two.snowflakecomputing.com":
						return fmt.Errorf("host = %v, want the new host", host)
					}

					return nil
				},
			},
		},
	})
}

func TestConnectionResourceDetectsDriftAndDeletion(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	file := filepath.Join(t.TempDir(), "trino.yaml")
	if err := os.WriteFile(file, []byte(trinoConfigurationYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	config := providerConfig(server.URL()) + connectionConfig("test", "trino", "Trino", file, trinoCredential)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllConnectionsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// A setting that is not configured is not Terraform's concern.
				PreConfig: func() { server.SetSetting("trino", "clientConfiguration", "catalogs", []any{"hive"}) },
				Config:    config,
				PlanOnly:  true,
			},
			{
				PreConfig:          func() { server.SetSetting("trino", "dataObjectSync", "schedule", "EVERY_6_HOURS") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying puts the configured value back, in place.
				Config: config,
				Check: checkLastConnectionPatch(server, 1, map[string]any{
					"dataObjectSync": map[string]any{"schedule": "EVERY_24_HOURS"},
				}),
			},
			{
				PreConfig:          func() { server.Remove("trino") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources["immuta_connection.test"]; ok {
						return errors.New("the deleted connection should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("immuta_connection.test", "state", "ACTIVE"),
			},
		},
	})
}

func TestConnectionResourceUnhealthyConnectionAndTeardown(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)

	server := testutil.NewConnectionServer()
	server.SettleAs("UNHEALTHY")
	server.ReturnTeardown(map[string]any{"script": "DROP ROLE IMMUTA_SYSTEM;"})
	t.Cleanup(server.Close)

	file := filepath.Join(t.TempDir(), "trino.yaml")
	if err := os.WriteFile(file, []byte(trinoConfigurationYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		// Teardown instructions are a warning, so destroy still succeeds.
		CheckDestroy: checkAllConnectionsDestroyed(server),
		Steps: []resource.TestStep{
			{
				// An unhealthy connection is saved and reported, not tainted.
				Config: providerConfig(server.URL()) + connectionConfig("test", "trino", "Trino", file, trinoCredential),
				Check:  resource.TestCheckResourceAttr("immuta_connection.test", "state", "UNHEALTHY"),
			},
		},
	})
}

func TestConnectionResourceValidatesAtPlanTime(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	resourceConfig := func(technology, configuration, credential string) string {
		return providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_connection" "test" {
  connection_key = "trino"
  technology     = %q
  configuration  = %q
  credential     = %s
}
`, technology, configuration, credential)
	}
	validCredential := "jsonencode(" + trinoCredential + ")"

	cases := map[string]struct {
		config string
		want   string
	}{
		"credential in the configuration": {
			resourceConfig("Trino", "clientConfiguration:\n  credential: {authenticationType: Token}\n", validCredential),
			`sensitive\s+credential\s+attribute`,
		},
		"connection key in the configuration": {
			resourceConfig("Trino", "connectionKey: other\n", validCredential),
			`connection_key\s+attribute`,
		},
		"credential without an authentication type": {
			resourceConfig("Trino", trinoConfigurationYAML, `jsonencode({ password = "secret" })`),
			`needs\s+an\s+authenticationType`,
		},
		"unknown technology": {
			resourceConfig("Postgres", trinoConfigurationYAML, validCredential),
			`value\s+must\s+be\s+one\s+of`,
		},
		"invalid yaml": {
			resourceConfig("Trino", "clientConfiguration: [unclosed\n", validCredential),
			`not\s+valid\s+YAML`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{Config: tc.config, PlanOnly: true, ExpectError: regexp.MustCompile(tc.want)},
				},
			})
		})
	}

	if got := len(server.Requests()); got != 0 {
		t.Errorf("the API received %d requests, want none for an invalid configuration", got)
	}
}

// checkLastConnectionPatch asserts the number of updates so far and, when
// want is not nil, the body of the last one.
func checkLastConnectionPatch(server *testutil.ConnectionServer, count int, want map[string]any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		patches := server.Patches()
		if len(patches) != count {
			return fmt.Errorf("updates = %d, want %d: %v", len(patches), count, patches)
		}
		if want != nil && !equalJSON(patches[len(patches)-1], want) {
			return fmt.Errorf("last update = %v, want %v", patches[len(patches)-1], want)
		}

		return nil
	}
}

func checkAllConnectionsDestroyed(server *testutil.ConnectionServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Count(); got != 0 {
			return fmt.Errorf("%d connections still exist after destroy", got)
		}

		return nil
	}
}
