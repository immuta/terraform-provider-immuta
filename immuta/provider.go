package immuta

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	frameworkschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/instacart/terraform-provider-immuta/client"
	"os"
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
			"immuta_purpose":  ResourcePurpose(),
			"immuta_project":  ResourceProject(),
			"immuta_bim_user": ResourceBimUser(),
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

////////////////////////////////////////////////
// terraform framework version of provider

// Ensure ScaffoldingProvider satisfies various provider interfaces.
var _ provider.Provider = &Provider{}

type Provider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

func New(version string) provider.Provider {
	return &Provider{
		version: version,
	}
}

func NewProvider(version string) func() provider.Provider {
	return func() provider.Provider {
		return New(version)
	}
}

func (p Provider) Metadata(ctx context.Context, request provider.MetadataRequest, response *provider.MetadataResponse) {
	response.TypeName = "immuta"
	response.Version = p.version
}

type ProviderModel struct {
	ApiToken types.String `tfsdk:"api_token"`
	Host     types.String `tfsdk:"host"`
}

func (p Provider) Schema(ctx context.Context, request provider.SchemaRequest, response *provider.SchemaResponse) {
	response.Schema = frameworkschema.Schema{
		Attributes: map[string]frameworkschema.Attribute{
			"api_key": frameworkschema.StringAttribute{
				Description: "The API key to access the endpoint. Can be set with IMMUTA_API_KEY.",
				Optional:    true,
			},
			"host": frameworkschema.StringAttribute{
				Description: "The endpoint to use. Can be set with IMMUTA_HOST.",
				Optional:    true,
			},
		},
	}
}

func (p Provider) Configure(ctx context.Context, request provider.ConfigureRequest, response *provider.ConfigureResponse) {
	config := ProviderModel{}

	apiKey := os.Getenv("IMMUTA_API_KEY")
	host := os.Getenv("IMMUTA_HOST")

	if config.ApiToken.ValueString() != "" {
		apiKey = config.ApiToken.ValueString()
	}

	if config.Host.ValueString() != "" {
		host = config.Host.ValueString()
	}

	if apiKey == "" {
		response.Diagnostics.AddError("api_token is required", "api_token is required")
	}

	if host == "" {
		response.Diagnostics.AddError("host is required", "host is required")
	}

	userAgent := fmt.Sprintf("HashiCorp Terraform/%s (+https://www.terraform.io) Terraform Plugin SDK/%s", "immuta", "immuta")

	immutaClient := client.NewClient(host, apiKey, userAgent)

	// todo validate client once low cost API call is available

	response.DataSourceData = immutaClient
	response.ResourceData = immutaClient
}

func (p Provider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p Provider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPurposeResource,
	}
}
