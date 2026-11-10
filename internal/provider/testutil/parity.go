package testutil

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

// Environment variables required by the acceptance and smoke tests.
const (
	EnvHost       = "IMMUTA_HOST"
	EnvAPIKey     = "IMMUTA_API_KEY"
	EnvNamePrefix = "IMMUTA_TEST_DOMAIN_NAME_PREFIX"

	// The Request app API, needed only by the data product tests.
	EnvRequestAppURL   = "IMMUTA_REQUEST_APP_URL"
	EnvRequestAppToken = "IMMUTA_REQUEST_APP_TOKEN"
)

// AccEnv holds validated acceptance test configuration. The API key is read
// from the environment when the client is built and is never stored here.
type AccEnv struct {
	Host       string
	NamePrefix string
}

// RequireAccEnv validates that every acceptance test environment variable is
// set, and fails the test with an actionable message when one is missing.
func RequireAccEnv(tb testing.TB) AccEnv {
	tb.Helper()

	var missing []string
	for _, name := range []string{EnvHost, EnvAPIKey, EnvNamePrefix} {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		tb.Fatalf("acceptance tests require %s to be set; use a dedicated sandbox tenant, never production", strings.Join(missing, ", "))
	}

	return AccEnv{
		Host:       strings.TrimSpace(os.Getenv(EnvHost)),
		NamePrefix: strings.TrimSpace(os.Getenv(EnvNamePrefix)),
	}
}

// AccClient builds an API client from the acceptance test environment. It is
// used to verify Terraform state against the API directly. The Request app
// API is configured too when its environment variables are set.
func AccClient(tb testing.TB) *immutaapi.Client {
	tb.Helper()

	RequireAccEnv(tb)

	client, err := immutaapi.NewClient(os.Getenv(EnvHost), os.Getenv(EnvAPIKey),
		immutaapi.WithRequestApp(os.Getenv(EnvRequestAppURL), os.Getenv(EnvRequestAppToken)))
	if err != nil {
		tb.Fatalf("building the Immuta API client: %s", err)
	}

	return client
}

// RequireDomain reads a domain directly from the API and fails the test when
// it cannot be read.
func RequireDomain(tb testing.TB, client *immutaapi.Client, id string) *immutaapi.Domain {
	tb.Helper()

	domain, err := client.GetDomain(context.Background(), id)
	if err != nil {
		tb.Fatalf("reading domain %s directly from the Immuta API: %s", id, err)
	}

	return domain
}

// RequireDomainAbsent asserts that the API reports the documented not-found
// response for a domain.
func RequireDomainAbsent(tb testing.TB, client *immutaapi.Client, id string) {
	tb.Helper()

	_, err := client.GetDomain(context.Background(), id)
	switch {
	case err == nil:
		tb.Fatalf("domain %s still exists in Immuta, expected it to be deleted", id)
	case !immutaapi.IsNotFound(err):
		tb.Fatalf("reading domain %s directly from the Immuta API: %s", id, err)
	}
}

// NormalizeAPIDomain reduces an API domain to the fields the provider owns.
// Server-generated and nondeterministic fields such as timestamps, the
// creating profile and tags are intentionally ignored.
func NormalizeAPIDomain(domain *immutaapi.Domain) map[string]any {
	autoscanning, ok := domain.Autoscanning()

	normalized := map[string]any{
		"id":              emptyToNil(domain.ID),
		"name":            emptyToNil(domain.Name),
		"description":     emptyToNil(domain.Description),
		"type":            emptyToNil(domain.Type),
		"assignment_type": emptyToNil(domain.AssignmentType),
		"autoscanning":    nil,
	}
	if ok {
		normalized["autoscanning"] = autoscanning
	}

	return normalized
}

// NormalizeStateDomain reduces Terraform state attributes to the same shape as
// NormalizeAPIDomain.
func NormalizeStateDomain(attributes map[string]string) map[string]any {
	normalized := map[string]any{}
	for _, key := range []string{"id", "name", "description", "type", "assignment_type"} {
		normalized[key] = emptyToNil(attributes[key])
	}

	switch attributes["autoscanning"] {
	case "true":
		normalized["autoscanning"] = true
	case "false":
		normalized["autoscanning"] = false
	default:
		normalized["autoscanning"] = nil
	}

	return normalized
}

// DomainParityError compares Terraform state with the Immuta API and returns
// an error describing any difference. It is suitable for use inside a
// Terraform test check function.
func DomainParityError(domain *immutaapi.Domain, attributes map[string]string) error {
	api := NormalizeAPIDomain(domain)
	state := NormalizeStateDomain(attributes)

	if !reflect.DeepEqual(api, state) {
		return fmt.Errorf("Terraform state and the Immuta API disagree:\n  api:   %s\n  state: %s", format(api), format(state))
	}

	return nil
}

// RequireDomainParity fails the test when Terraform state and the Immuta API
// disagree.
func RequireDomainParity(tb testing.TB, domain *immutaapi.Domain, attributes map[string]string) {
	tb.Helper()

	if err := DomainParityError(domain, attributes); err != nil {
		tb.Fatal(err)
	}
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func format(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, values[key]))
	}

	return strings.Join(parts, " ")
}
