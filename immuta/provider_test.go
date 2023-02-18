package immuta

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

var testAccProviders []*schema.Provider
var testAccProviderFactories func(providers *[]*schema.Provider) map[string]func() (*schema.Provider, error)

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
