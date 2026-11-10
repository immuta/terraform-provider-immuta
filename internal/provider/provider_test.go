package provider

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// testProviderFactories wires the provider under test into the Terraform test
// harness.
func testProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"immuta": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// skipWithoutTerraformCLI keeps the unit test suite runnable offline. The
// Terraform test harness needs a terraform binary, and downloads one when it
// is missing, which requires network access.
func skipWithoutTerraformCLI(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform is not installed, skipping the tests that drive the Terraform CLI")
	}
}

// clearProviderEnvironment makes a test independent of the developer's shell.
func clearProviderEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv(EnvHost, "")
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvRequestAppURL, "")
	t.Setenv(EnvRequestAppToken, "")
}

func providerConfig(host string) string {
	return fmt.Sprintf(`
provider "immuta" {
  host    = %q
  api_key = %q
}
`, host, testutil.TestAPIKey)
}

func TestProviderSchemaIsValid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	p := New("test")()

	metadata := &fwprovider.MetadataResponse{}
	p.Metadata(ctx, fwprovider.MetadataRequest{}, metadata)

	if metadata.TypeName != "immuta" {
		t.Errorf("provider type name = %q, want immuta", metadata.TypeName)
	}

	if metadata.Version != "test" {
		t.Errorf("provider version = %q, want test", metadata.Version)
	}

	schemaResponse := &fwprovider.SchemaResponse{}
	p.Schema(ctx, fwprovider.SchemaRequest{}, schemaResponse)

	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("building the provider schema produced errors: %v", schemaResponse.Diagnostics)
	}

	if diags := schemaResponse.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Errorf("the provider schema is not valid: %v", diags)
	}

	apiKey, ok := schemaResponse.Schema.Attributes["api_key"]
	if !ok {
		t.Fatal("the provider schema should expose an api_key attribute")
	}

	if !apiKey.IsSensitive() {
		t.Error("api_key must be marked sensitive so it is not shown in plan output")
	}

	requestAppToken, ok := schemaResponse.Schema.Attributes["request_app_token"]
	if !ok {
		t.Fatal("the provider schema should expose a request_app_token attribute")
	}
	if !requestAppToken.IsSensitive() {
		t.Error("request_app_token must be marked sensitive so it is not shown in plan output")
	}
}

func TestResourceSchemasAreValid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	for _, newResource := range New("test")().Resources(ctx) {
		response := &fwresource.SchemaResponse{}
		newResource().Schema(ctx, fwresource.SchemaRequest{}, response)

		if response.Diagnostics.HasError() {
			t.Fatalf("building the resource schema produced errors: %v", response.Diagnostics)
		}

		if diags := response.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("the resource schema is not valid: %v", diags)
		}
	}
}

func TestUserAgent(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		version string
		want    string
	}{
		"with a version":    {version: "1.2.3", want: "terraform-provider-immuta/1.2.3"},
		"without a version": {version: "", want: "terraform-provider-immuta/dev"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &immutaProvider{version: test.version}
			if got := p.userAgent(); got != test.want {
				t.Errorf("userAgent() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		values []string
		want   string
	}{
		"first value wins":              {values: []string{"a", "b"}, want: "a"},
		"blank values are skipped":      {values: []string{"   ", "b"}, want: "b"},
		"values are trimmed":            {values: []string{"  a  "}, want: "a"},
		"everything blank is empty":     {values: []string{"", "  "}, want: ""},
		"no values at all is also safe": {values: nil, want: ""},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := firstNonEmpty(test.values...); got != test.want {
				t.Errorf("firstNonEmpty(%q) = %q, want %q", test.values, got, test.want)
			}
		})
	}
}

func TestProviderConfigurationValidation(t *testing.T) {
	skipWithoutTerraformCLI(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	tests := map[string]struct {
		config      string
		expectError *regexp.Regexp
	}{
		"missing host": {
			config: fmt.Sprintf(`
provider "immuta" {
  api_key = %q
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, testutil.TestAPIKey),
			expectError: regexp.MustCompile(`Missing Immuta host`),
		},
		"missing api key": {
			config: fmt.Sprintf(`
provider "immuta" {
  host = %q
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, server.URL()),
			expectError: regexp.MustCompile(`Missing Immuta API key`),
		},
		"invalid request timeout": {
			config: fmt.Sprintf(`
provider "immuta" {
  host            = %q
  api_key         = %q
  request_timeout = "not-a-duration"
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, server.URL(), testutil.TestAPIKey),
			expectError: regexp.MustCompile(`Invalid request timeout`),
		},
		"zero request timeout": {
			config: fmt.Sprintf(`
provider "immuta" {
  host            = %q
  api_key         = %q
  request_timeout = "0s"
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, server.URL(), testutil.TestAPIKey),
			expectError: regexp.MustCompile(`Invalid request timeout`),
		},
		"retry count out of range": {
			config: fmt.Sprintf(`
provider "immuta" {
  host        = %q
  api_key     = %q
  retry_count = 42
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, server.URL(), testutil.TestAPIKey),
			expectError: regexp.MustCompile(`Invalid retry count`),
		},
		"request app url without a token": {
			config: fmt.Sprintf(`
provider "immuta" {
  host            = %q
  api_key         = %q
  request_app_url = "https://na.api.immutacloud.com/marketplace"
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, server.URL(), testutil.TestAPIKey),
			expectError: regexp.MustCompile(`Missing Immuta Request app token`),
		},
		"request app token without a url": {
			config: fmt.Sprintf(`
provider "immuta" {
  host              = %q
  api_key           = %q
  request_app_token = "a-token"
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, server.URL(), testutil.TestAPIKey),
			expectError: regexp.MustCompile(`Missing Immuta Request app URL`),
		},
		"invalid request app url": {
			config: fmt.Sprintf(`
provider "immuta" {
  host              = %q
  api_key           = %q
  request_app_url   = "ftp://na.api.immutacloud.com/marketplace"
  request_app_token = "a-token"
}

resource "immuta_domain" "test" {
  name = "Research"
}
`, server.URL(), testutil.TestAPIKey),
			expectError: regexp.MustCompile(`invalid\s+Request\s+app\s+API\s+base\s+URL`),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			clearProviderEnvironment(t)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      test.config,
						ExpectError: test.expectError,
					},
				},
			})
		})
	}
}

func TestProviderReadsCredentialsFromTheEnvironment(t *testing.T) {
	skipWithoutTerraformCLI(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	t.Setenv(EnvHost, server.URL())
	t.Setenv(EnvAPIKey, testutil.TestAPIKey)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
resource "immuta_domain" "test" {
  name = "Environment configured"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("immuta_domain.test", "id"),
					resource.TestCheckResourceAttr("immuta_domain.test", "name", "Environment configured"),
				),
			},
		},
	})
}
