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

// Environment variables for TestAccDataProductResource. A data product needs
// an existing domain, data source and subject matter expert, so the test is
// opt-in on top of the usual acceptance test variables.
const (
	envDataProductDomainID = "IMMUTA_TEST_DATA_PRODUCT_DOMAIN_ID"
	envDataProductSourceID = "IMMUTA_TEST_DATA_PRODUCT_SOURCE_ID"
	envDataProductExpertID = "IMMUTA_TEST_DATA_PRODUCT_EXPERT_ID"
	// Name of an existing request form, used for both request forms.
	envDataProductRequestForm = "IMMUTA_TEST_DATA_PRODUCT_REQUEST_FORM"
)

// TestAccDataProductResource creates, updates and deletes a data product on
// a real Immuta tenant, through the regional Request app API.
//
// Besides TF_ACC, IMMUTA_HOST, IMMUTA_API_KEY and
// IMMUTA_TEST_DOMAIN_NAME_PREFIX, it needs IMMUTA_REQUEST_APP_URL,
// IMMUTA_REQUEST_APP_TOKEN, IMMUTA_TEST_DATA_PRODUCT_DOMAIN_ID,
// IMMUTA_TEST_DATA_PRODUCT_SOURCE_ID, IMMUTA_TEST_DATA_PRODUCT_EXPERT_ID and
// IMMUTA_TEST_DATA_PRODUCT_REQUEST_FORM.
// Run it against a sandbox tenant, never production.
func TestAccDataProductResource(t *testing.T) {
	required := []string{
		testutil.EnvRequestAppURL, testutil.EnvRequestAppToken,
		envDataProductDomainID, envDataProductSourceID, envDataProductExpertID, envDataProductRequestForm,
	}
	for _, name := range required {
		if os.Getenv("TF_ACC") != "" && strings.TrimSpace(os.Getenv(name)) == "" {
			t.Skipf("set %s to run the data product acceptance test", strings.Join(required, ", "))
		}
	}

	name := acctest.RandomWithPrefix(accNamePrefix(t))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testutil.RequireAccEnv(t) },
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             testAccCheckDataProductsDestroyed(t),
		Steps: []resource.TestStep{
			{
				Config: testAccDataProductConfig(name, "Created by the provider acceptance tests"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_product.test", "name", name),
					testAccCheckDataProductParity(t),
				),
			},
			{
				Config:   testAccDataProductConfig(name, "Created by the provider acceptance tests"),
				PlanOnly: true,
			},
			{
				ResourceName:      "immuta_data_product.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Immuta does not return the request forms.
				ImportStateVerifyIgnore: []string{"data_access_request_form_id", "masking_exception_request_form_id"},
			},
			{
				Config: testAccDataProductConfig(name, "Updated by the provider acceptance tests"),
				Check:  testAccCheckDataProductParity(t),
			},
			{
				Config:   testAccDataProductConfig(name, "Updated by the provider acceptance tests"),
				PlanOnly: true,
			},
		},
	})
}

func testAccDataProductConfig(name, description string) string {
	return fmt.Sprintf(`
data "immuta_request_form" "test" {
  name = %q
}

resource "immuta_data_product" "test" {
  name                     = %q
  description              = %q
  subject_matter_expert_id = %q
  domains                  = [%q]
  data_sources             = [%q]

  data_access_request_form_id       = data.immuta_request_form.test.id
  masking_exception_request_form_id = data.immuta_request_form.test.id
}
`, os.Getenv(envDataProductRequestForm), name, description, os.Getenv(envDataProductExpertID), os.Getenv(envDataProductDomainID),
		os.Getenv(envDataProductSourceID))
}

// testAccCheckDataProductParity reads the data product and its data sources
// from the Immuta API and checks them against Terraform state.
func testAccCheckDataProductParity(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources["immuta_data_product.test"]
		if !ok {
			return fmt.Errorf("immuta_data_product.test is not in state")
		}
		id := resourceState.Primary.ID

		client := testutil.AccClient(t)
		product, err := client.GetDataProduct(context.Background(), id)
		if err != nil {
			return fmt.Errorf("reading data product %s from Immuta: %w", id, err)
		}
		sourceIDs, err := client.GetDataProductDataSources(context.Background(), id)
		if err != nil {
			return fmt.Errorf("reading the data sources of data product %s from Immuta: %w", id, err)
		}

		attributes := resourceState.Primary.Attributes
		description := ""
		if product.Description != nil {
			description = *product.Description
		}
		checks := map[string][2]string{
			"name":                     {product.Name, attributes["name"]},
			"description":              {description, attributes["description"]},
			"subject_matter_expert_id": {product.SubjectMatterExpertID(), attributes["subject_matter_expert_id"]},
			"domains":                  {strings.Join(product.Domains, ","), attributes["domains.0"]},
			"data_sources":             {strings.Join(sourceIDs, ","), attributes["data_sources.0"]},
		}
		for attribute, values := range checks {
			if values[0] != values[1] {
				return fmt.Errorf("%s: the Immuta API has %q, Terraform state has %q", attribute, values[0], values[1])
			}
		}

		return nil
	}
}

// testAccCheckDataProductsDestroyed asserts that every managed data product
// is gone from the tenant once the test has finished.
func testAccCheckDataProductsDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		client := testutil.AccClient(t)

		for name, resourceState := range state.RootModule().Resources {
			if resourceState.Type != "immuta_data_product" {
				continue
			}

			_, err := client.GetDataProduct(context.Background(), resourceState.Primary.ID)
			switch {
			case err == nil:
				return fmt.Errorf("%s (%s) still exists in Immuta after destroy", name, resourceState.Primary.ID)
			case !immutaapi.IsNotFound(err):
				return fmt.Errorf("reading data product %s from Immuta: %w", resourceState.Primary.ID, err)
			}
		}

		return nil
	}
}
