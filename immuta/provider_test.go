package immuta

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"os"
	"strings"
	"testing"
)

var testAccProviders []*schema.Provider
var testAccProviderFactories func(providers *[]*schema.Provider) map[string]func() (*schema.Provider, error)
var testAccProvider *schema.Provider

func init() {
	testAccProviders = []*schema.Provider{
		ImmutaProvider(),
	}
	testAccProviderFactories = func(providers *[]*schema.Provider) map[string]func() (*schema.Provider, error) {
		return map[string]func() (*schema.Provider, error){
			"immuta": func() (*schema.Provider, error) {
				p := ImmutaProvider()
				*providers = append(*providers, p)
				return p, nil
			},
		}
	}
}

func testAccPreCheck(t *testing.T) {
	if os.Getenv("IMMUTA_API_KEY") == "" {
		t.Fatal("Immuta API key must be set for acceptance tests")
	}

	endpoint := os.Getenv("IMMUTA_HOST")
	if endpoint == "" {
		t.Fatal("IMMUTA_HOST must be set for acceptance tests")
	}

	if !strings.Contains(endpoint, "dev-") {
		t.Fatal("Acceptance tests must be run against a dev environment")
	}
}
