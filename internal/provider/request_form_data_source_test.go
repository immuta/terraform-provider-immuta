package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// TestDataProductResourceRequestForms looks request forms up by name, sends
// them when the data product is created, and sends a changed one as an
// update. Immuta does not return them, so the configured values are kept.
func TestDataProductResourceRequestForms(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	standardID := server.AddRequestForm("Standard Request Form")
	maskingID := server.AddRequestForm("Masking Exception Form")

	config := func(maskingForm string) string {
		return dataProductProviderConfig(server) + fmt.Sprintf(`
data "immuta_request_form" "standard" {
  name = "Standard Request Form"
}

data "immuta_request_form" "masking" {
  name = "Masking Exception Form"
}

resource "immuta_data_product" "test" {
  id                       = %q
  name                     = "Sales"
  domains                  = [%q]
  subject_matter_expert_id = %q
  data_sources             = ["1"]

  data_access_request_form_id       = data.immuta_request_form.standard.id
  masking_exception_request_form_id = data.immuta_request_form.%s.id
}
`, testDataProductID, testDataProductDomain, testDataProductExpert, maskingForm)
	}

	checkStoredForms := func(dataAccessID, maskingExceptionID string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			definition, _, ok := server.Product(testDataProductID)
			if !ok {
				return fmt.Errorf("data product %s was not created", testDataProductID)
			}
			if definition["dataAccessRequestFormId"] != dataAccessID ||
				definition["maskingExceptionRequestFormId"] != maskingExceptionID {
				return fmt.Errorf("request forms sent to Immuta = %v and %v, want %s and %s",
					definition["dataAccessRequestFormId"], definition["maskingExceptionRequestFormId"], dataAccessID, maskingExceptionID)
			}

			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDataProductsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config("masking"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_request_form.standard", "id", standardID),
					resource.TestCheckResourceAttr("data.immuta_request_form.masking", "name", "Masking Exception Form"),
					resource.TestCheckResourceAttr("immuta_data_product.test", "data_access_request_form_id", standardID),
					resource.TestCheckResourceAttr("immuta_data_product.test", "masking_exception_request_form_id", maskingID),
					checkStoredForms(standardID, maskingID),
				),
			},
			{
				// Refreshing keeps the configured forms: not a change.
				Config:   config("masking"),
				PlanOnly: true,
			},
			{
				// One form for both: updated in place.
				Config: config("standard"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_data_product.test", "id", testDataProductID),
					resource.TestCheckResourceAttr("immuta_data_product.test", "masking_exception_request_form_id", standardID),
					checkStoredForms(standardID, standardID),
					func(*terraform.State) error {
						if got := len(server.Updates()); got != 1 {
							return fmt.Errorf("the API received %d updates, want 1", got)
						}

						return nil
					},
				),
			},
		},
	})
}

func TestRequestFormDataSourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDataProductServer()
	t.Cleanup(server.Close)

	server.AddRequestForm("Standard Request Form")
	server.AddRequestForm("Duplicate Form")
	server.AddRequestForm("Duplicate Form")

	cases := map[string]struct {
		provider, config, want string
	}{
		"no name": {
			config: `data "immuta_request_form" "test" {}`,
			want:   `(?s)Missing required argument.*name`,
		},
		"only a partial match": {
			config: `data "immuta_request_form" "test" {
  name = "Standard"
}`,
			want: `No\s+Request\s+app\s+request\s+form\s+is\s+named\s+"Standard"`,
		},
		"names are matched case-sensitively": {
			config: `data "immuta_request_form" "test" {
  name = "standard request form"
}`,
			want: `No\s+Request\s+app\s+request\s+form\s+is\s+named\s+"standard\s+request\s+form"`,
		},
		"several forms with the name": {
			config: `data "immuta_request_form" "test" {
  name = "Duplicate Form"
}`,
			want: `2\s+Request\s+app\s+request\s+forms\s+are\s+named\s+"Duplicate\s+Form"`,
		},
		"request app not configured": {
			provider: fmt.Sprintf(`provider "immuta" {
  host    = %q
  api_key = %q
}
`, server.URL(), testutil.TestAPIKey),
			config: `data "immuta_request_form" "test" {
  name = "Standard Request Form"
}`,
			want: `Immuta\s+Request\s+app\s+API\s+not\s+configured`,
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
				Steps:                    []resource.TestStep{{Config: provider + tc.config, ExpectError: regexp.MustCompile(tc.want)}},
			})
		})
	}
}
