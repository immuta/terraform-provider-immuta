package provider

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const (
	customConnectionKey = "snowflake-access"
	entraConnectionKey  = "entra-prod"
)

// accessGroupConfig declares a folder, optionally a securable in it, and
// optionally the binding of an Entra group to the securable.
func accessGroupConfig(server *testutil.ConnectionServer, securableName string, withSecurable, withBinding bool) string {
	config := providerConfig(server.URL()) + fmt.Sprintf(`
data "immuta_data_object" "group" {
  connection_key = %q
  path           = ["IMMUTA_SNOWFLAKE_ANALYST"]
}

resource "immuta_custom_data_object" "production" {
  connection_key = %q
  name           = "Production"
  type           = "FOLDER"
}
`, entraConnectionKey, customConnectionKey)

	if withSecurable {
		config += fmt.Sprintf(`
resource "immuta_custom_data_object" "analyst_access" {
  connection_key = %q
  name           = %q
  type           = "SECURABLE"
  parent_id      = immuta_custom_data_object.production.id
}
`, customConnectionKey, securableName)
	}

	if withBinding {
		config += `
resource "immuta_custom_securable_access_group" "analysts" {
  connection_key    = immuta_custom_data_object.analyst_access.connection_key
  securable_id      = immuta_custom_data_object.analyst_access.id
  identity_group_id = data.immuta_data_object.group.id
}
`
	}

	return config
}

func TestCustomSecurableAccessGroupResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDataObjectPolling(t, time.Second)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)
	server.AddConnection(customConnectionKey, "Custom")
	server.AddConnection(entraConnectionKey, "Entra")
	analysts := server.AddDataObjects(entraConnectionKey, testutil.DataObjectLevel{Name: "IMMUTA_SNOWFLAKE_ANALYST", Type: "IDENTITY_GROUP"})
	admins := server.AddDataObjects(entraConnectionKey, testutil.DataObjectLevel{Name: "IMMUTA_SNOWFLAKE_ADMIN", Type: "IDENTITY_GROUP"})

	// IDs from the state, recorded by the first step.
	var folderID, securableID string
	recordIDs := func(s *terraform.State) error {
		folderID = s.RootModule().Resources["immuta_custom_data_object.production"].Primary.ID
		securableID = s.RootModule().Resources["immuta_custom_data_object.analyst_access"].Primary.ID

		return nil
	}
	bound := func(want *string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if got := server.AccessGroup(securableID); got != *want {
				return fmt.Errorf("the securable %s is bound to %q, want %q", securableID, got, *want)
			}

			return nil
		}
	}
	securableImportID := func(*terraform.State) (string, error) { return customConnectionKey + "/" + securableID, nil }

	unbinds := func(want int) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if got := server.AccessGroupUnbinds(); got != want {
				return fmt.Errorf("%d access group DELETE requests, want %d", got, want)
			}

			return nil
		}
	}
	none := ""

	binding := accessGroupConfig(server, "Analyst Access", true, true)
	renamed := accessGroupConfig(server, "Analysts", true, true)
	unbound := accessGroupConfig(server, "Analysts", true, false)
	folderOnly := accessGroupConfig(server, "", false, false)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy: func(*terraform.State) error {
			for _, id := range []string{folderID, securableID} {
				if _, exists := server.DataObjectName(id); exists {
					return fmt.Errorf("the data object %s still exists after destroy", id)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: binding,
				Check: resource.ComposeAggregateTestCheckFunc(
					recordIDs,
					bound(&analysts),
					func(*terraform.State) error {
						if parent := server.DataObjectParent(securableID); parent != folderID {
							return fmt.Errorf("the securable is in %q, want the folder %s", parent, folderID)
						}

						return nil
					},
					resource.TestCheckResourceAttrPair("immuta_custom_securable_access_group.analysts", "identity_group_id", "data.immuta_data_object.group", "id"),
				),
			},
			{
				Config:   binding,
				PlanOnly: true,
			},
			{
				ResourceName:      "immuta_custom_securable_access_group.analysts",
				ImportState:       true,
				ImportStateIdFunc: securableImportID,
				ImportStateVerify: true,
			},
			{
				// The folder parent is read back on import.
				ResourceName:      "immuta_custom_data_object.analyst_access",
				ImportState:       true,
				ImportStateIdFunc: securableImportID,
				ImportStateVerify: true,
			},
			{
				// A rename keeps the securable, and so its binding.
				Config: renamed,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("immuta_custom_data_object.analyst_access", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("immuta_custom_securable_access_group.analysts", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPtr("immuta_custom_data_object.analyst_access", "id", &securableID),
					bound(&analysts),
					func(*terraform.State) error {
						if name, _ := server.DataObjectName(securableID); name != "Analysts" {
							return fmt.Errorf("the securable is named %q, want Analysts", name)
						}

						return nil
					},
				),
			},
			{
				// Destroying the binding unbinds the group.
				Config: unbound,
				Check: resource.ComposeAggregateTestCheckFunc(
					bound(&none),
					unbinds(1),
				),
			},
			{
				// Declared again, the binding is created again.
				Config: renamed,
				Check:  bound(&analysts),
			},
			{
				// Another group bound in Immuta is drift: the binding is
				// replaced, unbinding that group first.
				PreConfig: func() { server.SetAccessGroup(securableID, admins) },
				Config:    renamed,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("immuta_custom_securable_access_group.analysts", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					bound(&analysts),
					unbinds(2),
				),
			},
			{
				// A binding removed from the state stays bound.
				Config: unbound + `
removed {
  from = immuta_custom_securable_access_group.analysts

  lifecycle {
    destroy = false
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					bound(&analysts),
					unbinds(2),
				),
			},
			{
				// The same binding declared again is taken over.
				Config: renamed,
				Check:  bound(&analysts),
			},
			{
				// A binding to another group that Terraform does not manage is
				// not replaced silently.
				PreConfig: func() { server.SetAccessGroup(securableID, admins) },
				Config: unbound + `
removed {
  from = immuta_custom_securable_access_group.analysts

  lifecycle {
    destroy = false
  }
}
`,
			},
			{
				Config:      renamed,
				ExpectError: regexp.MustCompile(`already\s+has\s+another\s+access\s+group[\s\S]*terraform\s+import`),
			},
			{
				// Immuta refuses to delete a securable bound outside Terraform;
				// the diagnostic names the binding it reports.
				Config: folderOnly,
				ExpectError: regexp.MustCompile(`still\s+has\s+a\s+bound\s+access\s+group[\s\S]*accessGroup\s+reports\s+the\s+identity\s+group\s+` +
					regexp.QuoteMeta(admins) + `\s+of\s+the\s+connection\s+entra-prod\s+\(state\s+LIVE\)[\s\S]*immuta_custom_securable_access_group[\s\S]*Immuta\s+UI`),
			},
			{
				// Once the binding is removed in Immuta, the securable is deleted.
				PreConfig: func() { server.SetAccessGroup(securableID, "") },
				Config:    folderOnly,
				Check: func(*terraform.State) error {
					if _, exists := server.DataObjectName(securableID); exists {
						return fmt.Errorf("the securable %s still exists", securableID)
					}

					return nil
				},
			},
		},
	})
}

// A folder holding a bound securable cannot be deleted either.
func TestCustomDataObjectResourceFolderWithBoundSecurable(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDataObjectPolling(t, time.Second)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)
	server.AddConnection(customConnectionKey, "Custom")
	server.AddConnection(entraConnectionKey, "Entra")
	analysts := server.AddDataObjects(entraConnectionKey, testutil.DataObjectLevel{Name: "IMMUTA_SNOWFLAKE_ANALYST", Type: "IDENTITY_GROUP"})

	var securableID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: accessGroupConfig(server, "Analyst Access", true, false),
				Check: func(s *terraform.State) error {
					securableID = s.RootModule().Resources["immuta_custom_data_object.analyst_access"].Primary.ID

					return nil
				},
			},
			{
				// The securable is forgotten, then bound outside Terraform:
				// deleting the folder deletes everything under it.
				PreConfig: func() { server.SetAccessGroup(securableID, analysts) },
				Config: providerConfig(server.URL()) + `
removed {
  from = immuta_custom_data_object.analyst_access

  lifecycle {
    destroy = false
  }
}
`,
				ExpectError: regexp.MustCompile(`does\s+not\s+delete\s+a\s+folder\s+while\s+a\s+securable\s+in\s+it\s+still\s+has\s+a\s+bound\s+access\s+group`),
			},
			{
				PreConfig: func() { server.SetAccessGroup(securableID, "") },
				Config:    providerConfig(server.URL()),
			},
		},
	})
}

// Destroying a binding together with its securable unbinds the group first,
// so that Immuta deletes the securable.
func TestCustomSecurableAccessGroupResourceDestroyedWithSecurable(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDataObjectPolling(t, time.Second)

	server := testutil.NewConnectionServer()
	t.Cleanup(server.Close)
	server.AddConnection(customConnectionKey, "Custom")
	server.AddConnection(entraConnectionKey, "Entra")
	server.AddDataObjects(entraConnectionKey, testutil.DataObjectLevel{Name: "IMMUTA_SNOWFLAKE_ANALYST", Type: "IDENTITY_GROUP"})

	var securableID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: accessGroupConfig(server, "Analyst Access", true, true),
				Check: func(s *terraform.State) error {
					securableID = s.RootModule().Resources["immuta_custom_data_object.analyst_access"].Primary.ID

					return nil
				},
			},
			{
				Config: accessGroupConfig(server, "", false, false),
				Check: func(*terraform.State) error {
					if got := server.AccessGroupUnbinds(); got != 1 {
						return fmt.Errorf("%d access group DELETE requests, want 1", got)
					}
					if _, exists := server.DataObjectName(securableID); exists {
						return fmt.Errorf("the securable %s still exists", securableID)
					}

					return nil
				},
			},
		},
	})
}
