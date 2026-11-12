package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const (
	testAssetID          = "7d1d0c1e-5b0e-4a77-9a55-3b0f1c2d3e4f"
	testDecisionNodeID   = "6f1c2b9e-0a4d-4c55-9e3b-2d7a8f0c1b11"
	testAnalystsDecision = `@consumer.groups contains "analysts"`
)

// assetReviewFlowConfig reads the asset example review flow with
// templatefile(), as the example does.
func assetReviewFlowConfig(t *testing.T) string {
	t.Helper()

	return fmt.Sprintf(`
resource "immuta_asset_review_flow" "test" {
  asset_id     = %q
  request_type = "DATA_ACCESS"
  review_flow  = templatefile(%q, { request_form_id = %q })
}
`, testAssetID, exampleReviewFlowFile(t, "immuta_asset_review_flow", "analysts.yaml.tftpl"), testRequestFormID)
}

// checkAssetDecision checks the expression of the example's decision node
// as Immuta holds it.
func checkAssetDecision(server *testutil.DataProductServer, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		graph := server.AssetReviewFlow(testAssetID, "DATA_ACCESS")
		if graph == nil {
			return fmt.Errorf("the asset has no review flow of its own")
		}
		if len(graph) != 2 {
			return fmt.Errorf("the saved graph holds %v, want only nodes and edges", graph)
		}
		nodes, _ := graph["nodes"].([]any)
		edges, _ := graph["edges"].([]any)
		if len(nodes) != 5 || len(edges) != 4 {
			return fmt.Errorf("the saved graph has %d nodes and %d edges, want 5 and 4", len(nodes), len(edges))
		}
		for _, item := range nodes {
			node := item.(map[string]any)
			if node["type"] == "form" && node["requestFormId"] != testRequestFormID {
				return fmt.Errorf("requestFormId = %v, want the templated %s", node["requestFormId"], testRequestFormID)
			}
			if node["id"] == testDecisionNodeID && node["expression"] != want {
				return fmt.Errorf("expression = %v, want %s", node["expression"], want)
			}
		}

		return nil
	}
}

// changeAssetDecision changes the decision expression out of band.
func changeAssetDecision(server *testutil.DataProductServer, expression string) {
	graph := deepCopy(server.AssetReviewFlow(testAssetID, "DATA_ACCESS")).(map[string]any)
	for _, item := range graph["nodes"].([]any) {
		if node := item.(map[string]any); node["id"] == testDecisionNodeID {
			node["expression"] = expression
		}
	}
	server.SetAssetReviewFlow(testAssetID, "DATA_ACCESS", graph)
}

func TestAssetReviewFlowResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)
	server.AddAsset(testAssetID)

	config := dataProductProviderConfig(server) + assetReviewFlowConfig(t)
	inherited := map[string]any{
		"nodes": []any{map[string]any{"id": reviewFlowRootNodeID, "type": "root"}},
		"edges": []any{},
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy: func(*terraform.State) error {
			if server.AssetReviewFlow(testAssetID, "DATA_ACCESS") != nil {
				return fmt.Errorf("the review flow override still exists after destroy")
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_asset_review_flow.test", "id", testAssetID+"/DATA_ACCESS"),
					checkAssetDecision(server, testAnalystsDecision),
				),
			},
			{
				// The fields Immuta adds and its edge order cause no diff.
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:      "immuta_asset_review_flow.test",
				ImportState:       true,
				ImportStateId:     testAssetID + "/DATA_ACCESS",
				ImportStateVerify: true,
				// An import records Immuta's graph as JSON, not the file text.
				ImportStateVerifyIgnore: []string{"review_flow"},
			},
			{
				// A change made in the Request app is planned and undone.
				PreConfig: func() { changeAssetDecision(server, "true") },
				Config:    config,
				Check:     checkAssetDecision(server, testAnalystsDecision),
			},
			{
				// An override removed in the Request app is saved again, even
				// though the asset still inherits a review flow.
				PreConfig: func() {
					server.SetInheritedAssetReviewFlow(testAssetID, "DATA_ACCESS", "parent-asset", inherited)
					server.SetAssetReviewFlow(testAssetID, "DATA_ACCESS", nil)
				},
				Config: config,
				Check:  checkAssetDecision(server, testAnalystsDecision),
			},
		},
	})
}

func TestReviewFlowResourceRejectsInvalidGraphAtPlan(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: dataProductProviderConfig(server) + `
resource "immuta_asset_review_flow" "test" {
  asset_id     = "a1"
  request_type = "MASKING_EXCEPTION"
  review_flow  = yamlencode({ nodes = [{ id = "root", type = "root" }], edges = [] })
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`root\s+node\s+id\s+must\s+be\s+` + reviewFlowRootNodeID),
			},
			{
				Config: dataProductProviderConfig(server) + `
resource "immuta_asset_review_flow" "test" {
  asset_id     = "a1"
  request_type = "ACCESS"
  review_flow  = "nodes: []"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`DATA_ACCESS`),
			},
		},
	})
}

func TestDataProductReviewFlowResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	flowConfig := func(file string) string {
		return fmt.Sprintf(`
resource "immuta_data_product_review_flow" "test" {
  data_product_id = immuta_data_product.test.id
  request_type    = "MASKING_EXCEPTION"
  review_flow     = file(%q)
}
`, file)
	}
	provider := dataProductProviderConfig(server) + dataProductConfig("Sales", []string{"1"})
	autoApprove := provider + flowConfig(exampleReviewFlowFile(t, "immuta_data_product_review_flow", "auto_approve.yaml"))

	denyFile := t.TempDir() + "/deny.yaml"
	denyFlow := "nodes:\n  - {id: " + reviewFlowRootNodeID + ", type: root}\n  - {id: 4d6e8f0a-1b2c-4d3e-8f5a-6b7c8d9e0f44, type: outcome, outcome: deny}\n" +
		"edges:\n  - {source: " + reviewFlowRootNodeID + ", target: 4d6e8f0a-1b2c-4d3e-8f5a-6b7c8d9e0f44}\n"
	if err := os.WriteFile(denyFile, []byte(denyFlow), 0o600); err != nil {
		t.Fatal(err)
	}

	outcome := func(want string) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			id := s.RootModule().Resources["immuta_data_product.test"].Primary.ID
			graph := server.DataProductReviewFlow(id, "MASKING_EXCEPTION")
			if graph == nil {
				return fmt.Errorf("the data product has no masking exception review flow")
			}
			if got := graph["nodes"].([]any)[1].(map[string]any)["outcome"]; got != want {
				return fmt.Errorf("outcome = %v, want %s", got, want)
			}

			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: autoApprove,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("immuta_data_product_review_flow.test", "data_product_id", "immuta_data_product.test", "id"),
					outcome("approve"),
				),
			},
			{
				Config:   autoApprove,
				PlanOnly: true,
			},
			{
				// Changing the file updates the review flow in place.
				Config: provider + flowConfig(denyFile),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("immuta_data_product_review_flow.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: outcome("deny"),
			},
			{
				// Removing the resource keeps the review flow on the data product.
				Config: provider,
				Check:  outcome("deny"),
			},
		},
	})
}
