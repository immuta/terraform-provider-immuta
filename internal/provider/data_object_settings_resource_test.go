package provider

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const dataObjectConnectionKey = "acme_dev_databricks"

var (
	testCatalog = testutil.DataObjectLevel{Name: "acme_dev", Type: "CATALOG"}
	testSchema  = testutil.DataObjectLevel{Name: "sales", Type: "SCHEMA"}
	testTable   = testutil.DataObjectLevel{Name: "orders", Type: "TABLE"}
)

// fastDataObjectPolling shortens the wait for data objects to be
// registered. Tests using it must not run in parallel.
func fastDataObjectPolling(t *testing.T, timeout time.Duration) {
	t.Helper()

	interval, lookup := dataObjectPollInterval, dataObjectLookupTimeout
	dataObjectPollInterval, dataObjectLookupTimeout = 10*time.Millisecond, timeout
	t.Cleanup(func() { dataObjectPollInterval, dataObjectLookupTimeout = interval, lookup })
}

func dataObjectSettingsConfig(name, path string, syncPolicy bool) string {
	return fmt.Sprintf(`
resource "immuta_data_object_settings" %q {
  connection_key = immuta_connection.databricks.connection_key
  path           = %s
  sync_policy    = %t
}
`, name, path, syncPolicy)
}

func TestDataObjectSettingsResourceAfterConnection(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)
	fastDataObjectPolling(t, 5*time.Second)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	catalogID := server.AddDataObjects(dataObjectConnectionKey, testCatalog)
	schemaID := server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema)
	tableID := server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema, testTable)
	// The same names elsewhere, which must not be picked.
	server.AddDataObjects(dataObjectConnectionKey, testutil.DataObjectLevel{Name: "acme_prod", Type: "CATALOG"}, testSchema, testTable)
	server.AddDataObjects("other_connection", testCatalog, testSchema, testTable)
	// The connection's crawl has not finished when the first searches run.
	server.HideDataObjectsForSearches(3)

	connection := providerConfig(server.URL()) +
		exampleConnectionConfig("databricks", dataObjectConnectionKey, "Databricks", exampleConnectionFile(t, "databricks"), databricksCredential)
	config := func(tableSyncPolicy bool) string {
		return connection +
			dataObjectSettingsConfig("catalog", `["acme_dev"]`, true) +
			dataObjectSettingsConfig("schema", `["acme_dev", "sales"]`, true) +
			dataObjectSettingsConfig("table", `["acme_dev", "sales", "orders"]`, tableSyncPolicy)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy: func(*terraform.State) error {
			for _, id := range []string{catalogID, schemaID, tableID} {
				if server.DataObjectSyncPolicy(id) != nil {
					return fmt.Errorf("the syncPolicy of %s should have been cleared on destroy", id)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_object_settings.catalog", "id", catalogID),
					resource.TestCheckResourceAttr("immuta_data_object_settings.catalog", "type", "CATALOG"),
					resource.TestCheckResourceAttr("immuta_data_object_settings.schema", "id", schemaID),
					resource.TestCheckResourceAttr("immuta_data_object_settings.table", "id", tableID),
					resource.TestCheckResourceAttr("immuta_data_object_settings.table", "type", "TABLE"),
					resource.TestCheckResourceAttr("immuta_data_object_settings.table", "sync_policy", "true"),
					resource.TestCheckResourceAttr("immuta_data_object_settings.table", "sync_policy_inherited", "false"),
					checkDataObjectSyncPolicy(server, catalogID, true),
					checkDataObjectSyncPolicy(server, schemaID, true),
					checkDataObjectSyncPolicy(server, tableID, true),
					func(*terraform.State) error {
						// Waited for the crawl, then one search per data object.
						if got := server.DataObjectSearches(); got < 3+3 {
							return fmt.Errorf("data object searches = %d, want the crawl waited for", got)
						}
						// Only syncPolicy is sent: owners are left alone.
						for _, patch := range server.DataObjectPatches() {
							if patch["settings"] != `{"syncPolicy":true}` {
								return fmt.Errorf("patch = %v, want only syncPolicy", patch)
							}
						}

						return nil
					},
				),
			},
			{
				Config:   config(true),
				PlanOnly: true,
			},
			{
				// Turning it off for one table updates it in place.
				Config: config(false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_object_settings.table", "id", tableID),
					resource.TestCheckResourceAttr("immuta_data_object_settings.table", "sync_policy", "false"),
					checkDataObjectSyncPolicy(server, tableID, false),
					checkDataObjectSyncPolicy(server, schemaID, true),
				),
			},
			{
				ResourceName:      "immuta_data_object_settings.table",
				ImportState:       true,
				ImportStateId:     dataObjectConnectionKey + "/" + tableID,
				ImportStateVerify: true,
			},
		},
	})
}

func TestDataObjectSettingsResourceDriftAndRemoval(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)
	fastDataObjectPolling(t, 5*time.Second)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	catalogID := server.AddDataObjects(dataObjectConnectionKey, testCatalog)
	tableID := server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema, testTable)

	config := providerConfig(server.URL()) +
		exampleConnectionConfig("databricks", dataObjectConnectionKey, "Databricks", exampleConnectionFile(t, "databricks"), databricksCredential) +
		dataObjectSettingsConfig("table", `["acme_dev", "sales", "orders"]`, true)

	var recrawledID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// Turned off in Immuta.
				PreConfig:          func() { disabled := false; server.SetDataObjectSyncPolicy(tableID, &disabled) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check:  checkDataObjectSyncPolicy(server, tableID, true),
			},
			{
				// Cleared in Immuta, and inherited from a catalog without data
				// policies: a change.
				PreConfig: func() {
					disabled := false
					server.SetDataObjectSyncPolicy(catalogID, &disabled)
					server.SetDataObjectSyncPolicy(tableID, nil)
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkDataObjectSyncPolicy(server, tableID, true),
					resource.TestCheckResourceAttr("immuta_data_object_settings.table", "sync_policy_inherited", "false"),
				),
			},
			{
				// The table is dropped and crawled again with a new id: the
				// settings follow the path.
				PreConfig: func() {
					server.RemoveDataObject(tableID)
					recrawledID = server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema, testTable)
				},
				Config: config,
				Check: func(state *terraform.State) error {
					if got := state.RootModule().Resources["immuta_data_object_settings.table"].Primary.ID; got != recrawledID || got == tableID {
						return fmt.Errorf("id = %s, want the new data object %s", got, recrawledID)
					}

					return checkDataObjectSyncPolicy(server, recrawledID, true)(state)
				},
			},
			{
				// Gone with nothing to replace it: removed from state, and
				// destroy does not fail.
				PreConfig:          func() { server.RemoveDataObject(recrawledID) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources["immuta_data_object_settings.table"]; ok {
						return errors.New("the removed data object should have been removed from state")
					}

					return nil
				},
			},
		},
	})
}

func TestDataObjectSettingsResourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastConnectionPolling(t)
	fastDataObjectPolling(t, 200*time.Millisecond)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema, testTable)
	server.AddDataObjects(dataObjectConnectionKey, testutil.DataObjectLevel{Name: "acme_prod", Type: "CATALOG"}, testSchema, testTable)

	connection := providerConfig(server.URL()) +
		exampleConnectionConfig("databricks", dataObjectConnectionKey, "Databricks", exampleConnectionFile(t, "databricks"), databricksCredential)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// The schema level is missing from the path: the error shows
				// the paths that do exist.
				Config: connection + dataObjectSettingsConfig("table", `["acme_dev", "orders"]`, true),
				ExpectError: regexp.MustCompile(`(?s)no\s+data\s+object\s+with\s+the\s+path\s+acme_dev\.orders.*` +
					`acme_dev\.sales\.orders,\s+acme_prod\.sales\.orders`),
			},
			{
				Config:        connection + dataObjectSettingsConfig("imported", `["acme_dev"]`, true),
				ResourceName:  "immuta_data_object_settings.imported",
				ImportState:   true,
				ImportStateId: "no-separator",
				ExpectError:   regexp.MustCompile(`Expected\s+<connection_key>/<data\s+object\s+id>`),
			},
		},
	})
}

func TestDataObjectSettingsResourceValidation(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_data_object_settings" "empty" {
  connection_key = "acme_dev_databricks"
  path           = []
  sync_policy    = true
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`at\s+least\s+1`),
			},
		},
	})
	if got := len(server.Requests()); got != 0 {
		t.Errorf("the API received %d requests, want none for an invalid configuration", got)
	}
}

func checkDataObjectSyncPolicy(server *testutil.ConnectionServer, id string, want bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got := server.DataObjectSyncPolicy(id)
		if got == nil || *got != want {
			return fmt.Errorf("syncPolicy set on %s = %v, want %t", id, got, want)
		}

		return nil
	}
}
