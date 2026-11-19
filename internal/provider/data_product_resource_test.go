package provider

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const (
	testDataProductID     = "11111111-2222-4333-8444-555555555555"
	testDataProductDomain = "2ad33803-0a74-4ec6-8a1f-77693fb5f5fb"
	testDataProductExpert = "99aeadb5-352e-4872-9983-e628d399c36e"
	// Request forms are not looked up by the fake server, so any id works.
	testDataProductRequestForm = "cformrequest000000000001"
)

// dataProductConfig is an immuta_data_product block; extra holds optional
// attributes, one per line.
func dataProductConfig(name string, sourceIDs []string, extra ...string) string {
	quoted := make([]string, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		quoted = append(quoted, fmt.Sprintf("%q", sourceID))
	}

	return fmt.Sprintf(`
resource "immuta_data_product" "test" {
  name                     = %q
  domains                  = [%q]
  subject_matter_expert_id = %q
  data_sources             = [%s]

  data_access_request_form_id       = %q
  masking_exception_request_form_id = %q
  %s
}
`, name, testDataProductDomain, testDataProductExpert, strings.Join(quoted, ", "), testDataProductRequestForm,
		testDataProductRequestForm, strings.Join(extra, "\n  "))
}

// dataProductProviderConfig configures both the Govern API and the Request
// app API of a fake DataProductServer.
func dataProductProviderConfig(server *testutil.DataProductServer) string {
	return fmt.Sprintf(`
provider "immuta" {
  host              = %q
  api_key           = %q
  request_app_url   = %q
  request_app_token = %q
}
`, server.URL(), testutil.TestAPIKey, server.RequestAppURL(), testutil.TestRequestAppToken)
}

func TestDataProductResourceNeedsTheRequestApp(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	// Only the Govern API is configured: the error says what is missing,
	// and nothing is sent to Immuta.
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{{
			Config:      providerConfig(server.URL()) + dataProductConfig("Sales", []string{"1"}),
			ExpectError: regexp.MustCompile(`Immuta\s+Request\s+app\s+API\s+not\s+configured`),
		}},
	})
	if got := len(server.Requests()); got != 0 {
		t.Errorf("the API received %d requests, want none", got)
	}
}

func TestDataProductResourceFromEnvironment(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	t.Setenv(EnvHost, server.URL())
	t.Setenv(EnvAPIKey, testutil.TestAPIKey)
	t.Setenv(EnvRequestAppURL, server.RequestAppURL())
	t.Setenv(EnvRequestAppToken, testutil.TestRequestAppToken)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{{
			Config: dataProductConfig("Sales", []string{"1"}),
			Check:  resource.TestCheckResourceAttrSet("immuta_data_product.test", "id"),
		}},
	})
}

func TestDataProductResourceRejectedToken(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	// The Govern API key is not a Request app token.
	config := fmt.Sprintf(`
provider "immuta" {
  host              = %q
  api_key           = %q
  request_app_url   = %q
  request_app_token = %q
}
`, server.URL(), testutil.TestAPIKey, server.RequestAppURL(), testutil.TestAPIKey) + dataProductConfig("Sales", []string{"1"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{{
			Config:      config,
			ExpectError: regexp.MustCompile(`did\s+not\s+accept\s+the\s+personal\s+access\s+token`),
		}},
	})
}

func TestDataProductResourceFromDomainReferences(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)
	server.SetDomainDataSources(testDataProductDomain,
		testutil.DomainDataSource{ID: 1, Name: "Sales orders"},
		testutil.DomainDataSource{ID: 6, Name: "Sales customers"},
		testutil.DomainDataSource{ID: 2, Name: "HR people"},
	)

	config := dataProductProviderConfig(server) + fmt.Sprintf(`
data "immuta_domain_data_sources" "sales" {
  domain_id   = %q
  search_text = "sales"
}

resource "immuta_data_product" "test" {
  id                       = %q
  name                     = "Sales"
  description              = "Sales data"
  domains                  = [data.immuta_domain_data_sources.sales.domain_id]
  subject_matter_expert_id = %q
  data_sources             = data.immuta_domain_data_sources.sales.ids

  data_access_request_form_id       = %q
  masking_exception_request_form_id = %q
}
`, testDataProductDomain, testDataProductID, testDataProductExpert, testDataProductRequestForm, testDataProductRequestForm)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.sales", "ids.#", "2"),
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.sales", "ids.0", "1"),
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.sales", "data_sources.1.name", "Sales customers"),
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.sales", "data_sources.1.platform", "Snowflake"),
					// The id chosen in the configuration is kept.
					resource.TestCheckResourceAttr("immuta_data_product.test", "id", testDataProductID),
					resource.TestCheckTypeSetElemAttr("immuta_data_product.test", "domains.*", testDataProductDomain),
					resource.TestCheckResourceAttr("immuta_data_product.test", "data_sources.#", "2"),
					resource.TestCheckResourceAttr("immuta_data_product.test", "suspended", "false"),
					checkDataProductSources(server, testDataProductID, "1", "6"),
					func(*terraform.State) error {
						definition, _, _ := server.Product(testDataProductID)
						expert := definition["metadata"].(map[string]any)["subjectMatterExpert"].(map[string]any)
						if expert["id"] != testDataProductExpert || definition["description"] != "Sales data" {
							return fmt.Errorf("stored data product = %v", definition)
						}

						return nil
					},
				),
			},
			{
				// Data sources come back in another order: not a change.
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:      "immuta_data_product.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Immuta does not return the request forms.
				ImportStateVerifyIgnore: []string{"data_access_request_form_id", "masking_exception_request_form_id"},
			},
		},
	})
}

func TestDataProductResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	provider := dataProductProviderConfig(server)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{
			{
				// Without an id, Immuta assigns one.
				Config: provider + dataProductConfig("Sales", []string{"1", "2"}, `description = "Sales data"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("immuta_data_product.test", "id", func(value string) error {
						id = value
						if !uuidPattern.MatchString(value) {
							return fmt.Errorf("id = %q, want the UUID Immuta assigned", value)
						}

						return nil
					}),
					checkDataProductCalls(server, 0, 0),
				),
			},
			{
				// A rename only updates the data product.
				Config: provider + dataProductConfig("Sales EMEA", []string{"1", "2"}, `description = "EMEA sales"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_product.test", "name", "Sales EMEA"),
					checkDataProductCalls(server, 1, 0),
					func(*terraform.State) error {
						update := server.Updates()[0]
						if update["name"] != "Sales EMEA" || update["description"] != "EMEA sales" || update["metadata"] == nil || update["domains"] == nil {
							return fmt.Errorf("update = %v, want every managed field", update)
						}

						return nil
					},
				),
			},
			{
				// A new data source only replaces the data sources.
				Config: provider + dataProductConfig("Sales EMEA", []string{"1", "2", "3"}, `description = "EMEA sales"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkDataProductCalls(server, 1, 1),
					func(state *terraform.State) error {
						return checkDataProductSources(server, id, "1", "2", "3")(state)
					},
				),
			},
			{
				// Their order is not a change.
				Config:   provider + dataProductConfig("Sales EMEA", []string{"3", "2", "1"}, `description = "EMEA sales"`),
				PlanOnly: true,
			},
			{
				// Removing the description clears it in Immuta.
				Config: provider + dataProductConfig("Sales EMEA", []string{"1", "2", "3"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("immuta_data_product.test", "description"),
					checkDataProductCalls(server, 2, 1),
					func(*terraform.State) error {
						if update := server.Updates()[1]; update["description"] != nil {
							return fmt.Errorf("update = %v, want a null description", update)
						}

						return nil
					},
				),
			},
			{
				Config: provider + dataProductConfig("Sales EMEA", []string{"1", "2", "3"}, "suspended = true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_product.test", "suspended", "true"),
					checkDataProductCalls(server, 3, 1),
				),
			},
			{
				// Choosing an id is choosing another data product.
				Config: provider + dataProductConfig("Sales EMEA", []string{"1", "2", "3"}, fmt.Sprintf("id = %q", testDataProductID)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_product.test", "id", testDataProductID),
					resource.TestCheckResourceAttr("immuta_data_product.test", "suspended", "false"),
					func(*terraform.State) error {
						if _, _, ok := server.Product(id); ok || server.Count() != 1 {
							return errors.New("the data product with the old id should have been deleted")
						}

						return nil
					},
				),
			},
			{
				// Removing the id from the configuration keeps the data product.
				Config:   provider + dataProductConfig("Sales EMEA", []string{"1", "2", "3"}),
				PlanOnly: true,
			},
		},
	})
}

func TestDataProductResourceSuspendedAtCreation(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: dataProductProviderConfig(server) + dataProductConfig("Sales", []string{"1"}, fmt.Sprintf("id = %q", testDataProductID), "suspended = true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_product.test", "suspended", "true"),
					func(*terraform.State) error {
						if definition, _, _ := server.Product(testDataProductID); definition["suspended"] != true {
							return errors.New("the data product should have been suspended after creation")
						}

						return nil
					},
				),
			},
		},
	})
}

func TestDataProductResourceDetectsDriftAndDeletion(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	config := dataProductProviderConfig(server) + dataProductConfig("Sales", []string{"1", "2"}, fmt.Sprintf("id = %q", testDataProductID))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { server.Rename(testDataProductID, "Renamed in Immuta") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("immuta_data_product.test", "name", "Sales"),
			},
			{
				PreConfig:          func() { server.SetDataSources(testDataProductID, "1") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check:  checkDataProductSources(server, testDataProductID, "1", "2"),
			},
			{
				PreConfig:          func() { server.Remove(testDataProductID) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources["immuta_data_product.test"]; ok {
						return errors.New("the deleted data product should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check:  checkDataProductSources(server, testDataProductID, "1", "2"),
			},
		},
	})
}

func TestDataProductResourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	provider := dataProductProviderConfig(server)

	cases := map[string]struct {
		config string
		want   string
	}{
		"id not a uuid": {
			provider + dataProductConfig("Sales", []string{"1"}, `id = "sales"`),
			`must\s+be\s+a\s+UUID`,
		},
		"expert not a uuid": {
			provider + strings.Replace(dataProductConfig("Sales", []string{"1"}), testDataProductExpert, "42", 1),
			`must\s+be\s+a\s+UUID`,
		},
		"no domain": {
			provider + strings.Replace(dataProductConfig("Sales", []string{"1"}), fmt.Sprintf("[%q]", testDataProductDomain), "[]", 1),
			`domains\s+set\s+must\s+contain\s+at\s+least\s+1`,
		},
		"empty name": {
			provider + dataProductConfig("", []string{"1"}),
			`length\s+must\s+be\s+between\s+1\s+and\s+255`,
		},
		"empty data source id": {
			provider + dataProductConfig("Sales", []string{""}),
			`length\s+must\s+be\s+at\s+least\s+1`,
		},
		"only one request form": {
			provider + strings.Replace(dataProductConfig("Sales", []string{"1"}),
				fmt.Sprintf("masking_exception_request_form_id = %q", testDataProductRequestForm), "", 1),
			`(?s)masking_exception_request_form_id.*must\s+be\s+specified`,
		},
		// Immuta requires request forms on create: caught at plan time.
		"no request forms": {
			provider + strings.NewReplacer(
				fmt.Sprintf("data_access_request_form_id       = %q", testDataProductRequestForm), "",
				fmt.Sprintf("masking_exception_request_form_id = %q", testDataProductRequestForm), "",
			).Replace(dataProductConfig("Sales", []string{"1"})),
			`(?s)Request\s+forms\s+required.*data_access_request_form_id\s+and\s+masking_exception_request_form_id`,
		},
		"empty request form id": {
			provider + strings.Replace(dataProductConfig("Sales", []string{"1"}),
				fmt.Sprintf("data_access_request_form_id       = %q", testDataProductRequestForm), `data_access_request_form_id = ""`, 1),
			`length\s+must\s+be\s+at\s+least\s+1`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps:                    []resource.TestStep{{Config: tc.config, PlanOnly: true, ExpectError: regexp.MustCompile(tc.want)}},
			})
		})
	}
	if got := len(server.Requests()); got != 0 {
		t.Errorf("the API received %d requests, want none for an invalid configuration", got)
	}

	// Two data products with the same name: Immuta refuses the second, and
	// the error says why.
	second := strings.Replace(dataProductConfig("Sales", []string{"1"}), `"test"`, `"second"`, 1)
	second = strings.Replace(second, "data_sources", "depends_on               = [immuta_data_product.test]\n  data_sources", 1)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config:      provider + dataProductConfig("Sales", []string{"1"}) + second,
				ExpectError: regexp.MustCompile(`name\s+is\s+already\s+used`),
			},
		},
	})
}

func TestDomainDataSourcesDataSourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)
	server.SetDomainDataSources(testDataProductDomain)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// A domain without data sources is an empty list, not an error.
				Config: providerConfig(server.URL()) + fmt.Sprintf("data \"immuta_domain_data_sources\" \"empty\" {\n  domain_id = %q\n}\n", testDataProductDomain),
				Check:  resource.TestCheckResourceAttr("data.immuta_domain_data_sources.empty", "ids.#", "0"),
			},
			{
				Config:      providerConfig(server.URL()) + "data \"immuta_domain_data_sources\" \"missing\" {\n  domain_id = \"missing\"\n}\n",
				ExpectError: regexp.MustCompile(`Unable\s+to\s+list\s+the\s+data\s+sources\s+of\s+the\s+Immuta\s+domain\s+missing`),
			},
		},
	})
}

func checkDataProductCalls(server *testutil.DataProductServer, updates, dataSourceUpdates int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := len(server.Updates()); got != updates {
			return fmt.Errorf("data product updates = %d, want %d: %v", got, updates, server.Updates())
		}
		if got := server.DataSourceUpdates(); got != dataSourceUpdates {
			return fmt.Errorf("data source updates = %d, want %d", got, dataSourceUpdates)
		}

		return nil
	}
}

func checkDataProductSources(server *testutil.DataProductServer, id string, want ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		_, sourceIDs, ok := server.Product(id)
		if !ok || !sameStrings(sourceIDs, want) {
			return fmt.Errorf("data sources of %s = %v, want %v", id, sourceIDs, want)
		}

		return nil
	}
}

// sameStrings reports whether two lists hold the same strings, in any order.
func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	counts := map[string]int{}
	for _, value := range got {
		counts[value]++
	}
	for _, value := range want {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}

	return true
}

func checkAllDataProductsDestroyed(server *testutil.DataProductServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Count(); got != 0 {
			return fmt.Errorf("%d data products still exist after destroy", got)
		}

		return nil
	}
}
