package provider

import (
	"fmt"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const (
	testGoldTagID = 192
	testPIITagID  = 7
)

func dataObjectTagConfig(serverURL, tagID string) string {
	return providerConfig(serverURL) + fmt.Sprintf(`
data "immuta_data_object" "orders" {
  connection_key = %q
  path           = ["acme_dev", "sales", "orders"]
}

resource "immuta_data_object_tag" "gold" {
  connection_key = data.immuta_data_object.orders.connection_key
  data_object_id = data.immuta_data_object.orders.id
  tag_id         = %q
}
`, dataObjectConnectionKey, tagID)
}

func checkDataObjectTags(server *testutil.ConnectionServer, id string, want ...int64) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.DataObjectTags(id); !slices.Equal(got, want) {
			return fmt.Errorf("tags applied to %s = %v, want %v", id, got, want)
		}

		return nil
	}
}

func TestDataObjectTagResource(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDataObjectPolling(t, 5*time.Second)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	server.AddTag(testGoldTagID, "Access.Layer.Gold")
	server.AddTag(testPIITagID, "Discovered.PII")
	schemaID := server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema)
	tableID := server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema, testTable)
	// A table with the same name elsewhere, which must not be picked.
	server.AddDataObjects(dataObjectConnectionKey, testutil.DataObjectLevel{Name: "acme_prod", Type: "CATALOG"}, testSchema, testTable)
	// A tag Terraform does not manage, which must be left alone.
	server.ApplyDataObjectTag(tableID, testPIITagID)
	// The same tag on the parent schema: the table only inherits it, so it
	// still has to be applied to the table itself.
	server.ApplyDataObjectTag(schemaID, testGoldTagID)

	config := dataObjectTagConfig(server.URL(), "192")
	wantID := fmt.Sprintf("%s/%s/%d", dataObjectConnectionKey, tableID, testGoldTagID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy: resource.ComposeAggregateTestCheckFunc(
			// Only the managed tag is removed.
			checkDataObjectTags(server, tableID, testPIITagID),
			checkDataObjectTags(server, schemaID, testGoldTagID),
		),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_data_object.orders", "id", tableID),
					resource.TestCheckResourceAttr("data.immuta_data_object.orders", "name", "orders"),
					resource.TestCheckResourceAttr("data.immuta_data_object.orders", "type", "TABLE"),
					resource.TestCheckResourceAttr("immuta_data_object_tag.gold", "id", wantID),
					resource.TestCheckResourceAttr("immuta_data_object_tag.gold", "data_object_id", tableID),
					resource.TestCheckResourceAttr("immuta_data_object_tag.gold", "tag_id", "192"),
					resource.TestCheckResourceAttr("immuta_data_object_tag.gold", "tag_name", "Access.Layer.Gold"),
					checkDataObjectTags(server, tableID, testPIITagID, testGoldTagID),
					checkDataObjectTags(server, schemaID, testGoldTagID),
					func(*terraform.State) error {
						// One documented tags patch, adding only the managed tag.
						want := []testutil.DataObjectTagPatch{{ID: tableID, Add: []int64{testGoldTagID}}}
						if got := server.DataObjectTagPatches(); fmt.Sprint(got) != fmt.Sprint(want) {
							return fmt.Errorf("tag patches = %+v, want %+v", got, want)
						}
						// No settings were sent along with the tags.
						if got := server.DataObjectPatches(); len(got) != 0 {
							return fmt.Errorf("settings patches = %v, want none", got)
						}

						return nil
					},
				),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:      "immuta_data_object_tag.gold",
				ImportState:       true,
				ImportStateId:     wantID,
				ImportStateVerify: true,
			},
			{
				// Removed in Immuta: the next apply applies it again.
				PreConfig: func() { server.RemoveDataObjectTag(tableID, testGoldTagID) },
				Config:    config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_object_tag.gold", "id", wantID),
					checkDataObjectTags(server, tableID, testPIITagID, testGoldTagID),
				),
			},
		},
	})
}

func TestDataObjectTagResourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDataObjectPolling(t, 50*time.Millisecond)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)

	server.AddTag(testGoldTagID, "Access.Layer.Gold")
	tableID := server.AddDataObjects(dataObjectConnectionKey, testCatalog, testSchema, testTable)
	// Applied outside Terraform: adopting it silently would remove it on
	// destroy.
	server.ApplyDataObjectTag(tableID, testGoldTagID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      dataObjectTagConfig(server.URL(), "192"),
				ExpectError: regexp.MustCompile(`(?s)already applied.*terraform import.*` + regexp.QuoteMeta(dataObjectConnectionKey+"/"+tableID+"/192")),
			},
			{
				// Immuta rejects a tag that does not exist.
				Config:      dataObjectTagConfig(server.URL(), "999"),
				ExpectError: regexp.MustCompile(`Unable to apply the tag 999`),
			},
			{
				Config:      dataObjectTagConfig(server.URL(), "Access.Layer.Gold"),
				ExpectError: regexp.MustCompile(`must be a numeric tag ID`),
			},
			{
				Config: providerConfig(server.URL()) + fmt.Sprintf(`
data "immuta_data_object" "missing" {
  connection_key = %q
  path           = ["acme_dev", "sales", "missing"]
}
`, dataObjectConnectionKey),
				ExpectError: regexp.MustCompile(`Immuta data object not found`),
			},
		},
	})

	// Nothing was removed by the failed applies.
	if got := server.DataObjectTags(tableID); !slices.Equal(got, []int64{testGoldTagID}) {
		t.Errorf("tags applied to the table = %v, want the pre-existing tag kept", got)
	}
}

func TestTagDataSource(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)
	server.SeedTag("Access.Layer.Gold")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
data "immuta_tag" "gold" {
  name = "Access.Layer.Gold"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_tag.gold", "name", "Access.Layer.Gold"),
					resource.TestMatchResourceAttr("data.immuta_tag.gold", "id", regexp.MustCompile(`^[0-9]+$`)),
					resource.TestCheckResourceAttr("data.immuta_tag.gold", "source", "curated"),
					resource.TestCheckResourceAttr("data.immuta_tag.gold", "system_created", "false"),
				),
			},
			{
				// The match is exact: a parent tag's name with a different case is
				// not found.
				Config: providerConfig(server.URL()) + `
data "immuta_tag" "gold" {
  name = "access.layer.gold"
}
`,
				ExpectError: regexp.MustCompile(`No Immuta tag found`),
			},
		},
	})
}
