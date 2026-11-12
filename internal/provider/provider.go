// Package provider implements the Terraform provider for the documented
// Immuta SaaS API.
package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

const (
	// EnvHost is the environment variable used when host is not configured.
	EnvHost = "IMMUTA_HOST"
	// EnvAPIKey is the environment variable used when api_key is not configured.
	EnvAPIKey = "IMMUTA_API_KEY"
	// EnvRequestAppURL is the environment variable used when request_app_url
	// is not configured.
	EnvRequestAppURL = "IMMUTA_REQUEST_APP_URL"
	// EnvRequestAppToken is the environment variable used when
	// request_app_token is not configured.
	EnvRequestAppToken = "IMMUTA_REQUEST_APP_TOKEN"
)

var (
	_ provider.Provider              = (*immutaProvider)(nil)
	_ provider.ProviderWithFunctions = (*immutaProvider)(nil)
)

type immutaProvider struct {
	version string
}

// New returns the provider implementation for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &immutaProvider{version: version}
	}
}

type immutaProviderModel struct {
	Host            types.String `tfsdk:"host"`
	APIKey          types.String `tfsdk:"api_key"`
	RequestAppURL   types.String `tfsdk:"request_app_url"`
	RequestAppToken types.String `tfsdk:"request_app_token"`
	RequestTimeout  types.String `tfsdk:"request_timeout"`
	RetryCount      types.Int64  `tfsdk:"retry_count"`
}

func (p *immutaProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "immuta"
	resp.Version = p.version
}

func (p *immutaProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage Immuta SaaS objects with the documented Immuta Govern API.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Base URL of the Immuta tenant, for example `https://example.immuta.com`. May also be set with the `" + EnvHost + "` environment variable.",
			},
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Immuta API key sent in the `Authorization` header. May also be set with the `" + EnvAPIKey + "` environment variable, which is the recommended way to supply it.",
			},
			"request_app_url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Regional base URL of the Request app (formerly Marketplace) API, used by `immuta_data_product`: " +
					"`https://na.api.immutacloud.com/marketplace`, `https://eu.api.immutacloud.com/marketplace` or " +
					"`https://ap.api.immutacloud.com/marketplace`, matching the region of the tenant. May also be set with the `" +
					EnvRequestAppURL + "` environment variable. Required with `request_app_token`.",
			},
			"request_app_token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Immuta personal access token (PAT) for the Request app API, sent as a bearer token. " +
					"It is separate from `api_key`, which the Request app does not accept. May also be set with the `" +
					EnvRequestAppToken + "` environment variable, which is the recommended way to supply it. Required with `request_app_url`.",
			},
			"request_timeout": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Timeout for a single API request, as a Go duration such as `30s`. Defaults to `%s`.", immutaapi.DefaultRequestTimeout),
			},
			"retry_count": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: fmt.Sprintf("Number of retries for idempotent requests that fail with a transient error such as HTTP 429, 502, 503 or 504. Must be between 0 and %d. Defaults to %d.", immutaapi.MaxRetryCount, immutaapi.DefaultRetryCount),
			},
		},
	}
}

func (p *immutaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config immutaProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	unknownAttributes := []struct {
		name  string
		value attr.Value
	}{
		{"host", config.Host},
		{"api_key", config.APIKey},
		{"request_app_url", config.RequestAppURL},
		{"request_app_token", config.RequestAppToken},
		{"request_timeout", config.RequestTimeout},
		{"retry_count", config.RetryCount},
	}

	for _, attribute := range unknownAttributes {
		if attribute.value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root(attribute.name),
				"Unknown Immuta provider configuration",
				fmt.Sprintf("The provider cannot be configured because %q is not known at plan time. Set it to a static value, or supply it with an environment variable.", attribute.name),
			)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	host := firstNonEmpty(config.Host.ValueString(), os.Getenv(EnvHost))
	apiKey := firstNonEmpty(config.APIKey.ValueString(), os.Getenv(EnvAPIKey))
	requestAppURL := firstNonEmpty(config.RequestAppURL.ValueString(), os.Getenv(EnvRequestAppURL))
	requestAppToken := firstNonEmpty(config.RequestAppToken.ValueString(), os.Getenv(EnvRequestAppToken))

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Immuta host",
			fmt.Sprintf("Set the provider `host` argument or the %s environment variable to the base URL of the Immuta tenant, for example https://example.immuta.com.", EnvHost),
		)
	}

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Immuta API key",
			fmt.Sprintf("Set the provider `api_key` argument or the %s environment variable. Prefer the environment variable so the credential is not stored in configuration or state.", EnvAPIKey),
		)
	}

	// The Request app is optional, as only immuta_data_product needs it, but
	// half a configuration is a mistake.
	if requestAppURL != "" && requestAppToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("request_app_token"),
			"Missing Immuta Request app token",
			fmt.Sprintf("A Request app URL is configured, but no token. Set the provider `request_app_token` argument or the %s environment variable to an Immuta personal access token.", EnvRequestAppToken),
		)
	}
	if requestAppToken != "" && requestAppURL == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("request_app_url"),
			"Missing Immuta Request app URL",
			fmt.Sprintf("A Request app token is configured, but no URL. Set the provider `request_app_url` argument or the %s environment variable to the regional Request app API URL, for example https://na.api.immutacloud.com/marketplace.", EnvRequestAppURL),
		)
	}

	timeout := immutaapi.DefaultRequestTimeout
	if value := strings.TrimSpace(config.RequestTimeout.ValueString()); value != "" {
		parsed, err := time.ParseDuration(value)
		switch {
		case err != nil:
			resp.Diagnostics.AddAttributeError(
				path.Root("request_timeout"),
				"Invalid request timeout",
				fmt.Sprintf("%q is not a valid Go duration. Use a value such as 30s or 2m.", value),
			)
		case parsed <= 0:
			resp.Diagnostics.AddAttributeError(
				path.Root("request_timeout"),
				"Invalid request timeout",
				"The request timeout must be greater than zero so that API calls cannot hang indefinitely.",
			)
		default:
			timeout = parsed
		}
	}

	retryCount := immutaapi.DefaultRetryCount
	if !config.RetryCount.IsNull() {
		value := config.RetryCount.ValueInt64()
		if value < 0 || value > immutaapi.MaxRetryCount {
			resp.Diagnostics.AddAttributeError(
				path.Root("retry_count"),
				"Invalid retry count",
				fmt.Sprintf("The retry count must be between 0 and %d, got %d.", immutaapi.MaxRetryCount, value),
			)
		} else {
			retryCount = int(value)
		}
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Defence in depth: never emit the credentials through provider logs.
	secrets := []string{apiKey}
	if requestAppToken != "" {
		secrets = append(secrets, requestAppToken)
	}
	ctx = tflog.MaskAllFieldValuesStrings(ctx, secrets...)
	ctx = tflog.MaskMessageStrings(ctx, secrets...)

	client, err := immutaapi.NewClient(
		host,
		apiKey,
		immutaapi.WithRequestTimeout(timeout),
		immutaapi.WithRetryCount(retryCount),
		immutaapi.WithUserAgent(p.userAgent()),
		immutaapi.WithRequestApp(requestAppURL, requestAppToken),
	)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create the Immuta API client",
			err.Error(),
		)

		return
	}

	tflog.Debug(ctx, "Configured the Immuta API client", map[string]any{
		"host":            client.Host(),
		"request_app_url": client.RequestAppURL(),
		"request_timeout": timeout.String(),
		"retry_count":     retryCount,
	})

	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *immutaProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDomainResource,
		NewDomainIdentifierResource,
		NewFrameworkResource,
		NewGlobalPolicyResource,
		NewConnectionResource,
		NewDataProductResource,
		NewDataObjectSettingsResource,
		NewDomainDynamicAssignmentResource,
		NewDomainDataSourceAssignmentResource,
		NewGroupResource,
		NewIdentifierResource,
		NewGroupAttributeResource,
		NewGroupUserResource,
		NewUserResource,
		NewUserAttributeResource,
		NewTagResource,
		NewDataObjectTagResource,
		NewCustomDataObjectResource,
		NewCustomSecurableAccessGroupResource,
		NewAssetReviewFlowResource,
		NewDataProductReviewFlowResource,
	}
}

func (p *immutaProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDomainDataSourcesDataSource,
		NewUserDataSource,
		NewGlobalUserDataSource,
		NewRequestFormDataSource,
		NewDataSourceDataSource,
		NewTagDataSource,
		NewDataObjectDataSource,
	}
}

// Functions returns the provider-defined functions. They need Terraform 1.8
// or later; the rest of the provider does not.
func (p *immutaProvider) Functions(_ context.Context) []func() function.Function {
	return []func() function.Function{
		NewDecodeFrameworkRulesFunction,
	}
}

func (p *immutaProvider) userAgent() string {
	version := p.version
	if version == "" {
		version = "dev"
	}

	return fmt.Sprintf("terraform-provider-immuta/%s", version)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}
