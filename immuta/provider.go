package immuta

import (
	"context"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func ImmutaProvider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"api_key": {
				Type:        schema.TypeString,
				DefaultFunc: schema.EnvDefaultFunc("IMMUTA_API_KEY", nil),
				Description: "The API token to access the endpoint. Can be set with IMMUTA_API_KEY.",
				Required:    true,
			},
			"host": {
				Type:        schema.TypeString,
				DefaultFunc: schema.EnvDefaultFunc("IMMUTA_HOST", nil),
				Description: "The endpoint to use. Can be set with IMMUTA_HOST.",
				Required:    true,
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"immuta_purpose": ResourcePurpose(),
			"immuta_project": ResourceProject(),
		},
		ConfigureContextFunc: providerConfigure,
	}
}

func providerConfigure(ctx context.Context, d *schema.ResourceData) (client interface{}, diags diag.Diagnostics) {
	config := Config{
		APIKey: d.Get("api_key").(string),
		Host:   d.Get("host").(string),
	}
	client, err := config.ImmutaClient()
	if err != nil {
		return nil, diag.FromErr(err)
	}

	return
}
