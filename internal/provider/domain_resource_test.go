package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const domainResourceName = "immuta_domain.test"

func TestNormalizeOptionalString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		apiValue string
		prior    types.String
		want     types.String
	}{
		"api value is used": {
			apiValue: "Research domain",
			prior:    types.StringNull(),
			want:     types.StringValue("Research domain"),
		},
		"api value replaces a stale prior value": {
			apiValue: "Changed remotely",
			prior:    types.StringValue("Research domain"),
			want:     types.StringValue("Changed remotely"),
		},
		"omitted value stays null when it was never configured": {
			apiValue: "",
			prior:    types.StringNull(),
			want:     types.StringNull(),
		},
		"omitted value stays null when the prior value is unknown": {
			apiValue: "",
			prior:    types.StringUnknown(),
			want:     types.StringNull(),
		},
		"an explicitly configured empty string is preserved": {
			apiValue: "",
			prior:    types.StringValue(""),
			want:     types.StringValue(""),
		},
		"a remotely cleared value is reported as drift": {
			apiValue: "",
			prior:    types.StringValue("Research domain"),
			want:     types.StringValue(""),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeOptionalString(test.apiValue, test.prior); !got.Equal(test.want) {
				t.Errorf("normalizeOptionalString(%q, %s) = %s, want %s", test.apiValue, test.prior, got, test.want)
			}
		})
	}
}

func TestKnownStringOr(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		apiValue string
		prior    types.String
		want     types.String
	}{
		"api value wins":                    {apiValue: "domain", prior: types.StringValue("domain"), want: types.StringValue("domain")},
		"configured value is kept":          {apiValue: "", prior: types.StringValue("domain"), want: types.StringValue("domain")},
		"unknown collapses to null":         {apiValue: "", prior: types.StringUnknown(), want: types.StringNull()},
		"null stays null when API omits it": {apiValue: "", prior: types.StringNull(), want: types.StringNull()},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := knownStringOr(test.apiValue, test.prior); !got.Equal(test.want) {
				t.Errorf("knownStringOr(%q, %s) = %s, want %s", test.apiValue, test.prior, got, test.want)
			}
		})
	}
}

func TestApplyDomain(t *testing.T) {
	t.Parallel()

	createdBy := int64(2)
	autoscanning := true

	t.Run("a fully populated domain", func(t *testing.T) {
		t.Parallel()

		model := domainResourceModel{Description: types.StringValue("Research domain")}
		model.applyDomain(&immutaapi.Domain{
			ID:             "abc",
			Name:           "Research",
			Description:    "Research domain",
			Type:           "domain",
			AssignmentType: "manual",
			Settings:       &immutaapi.DomainSettings{Autoscanning: &autoscanning},
			CreatedBy:      &createdBy,
			CreatedAt:      "2024-01-01T00:00:00.000Z",
			UpdatedAt:      "2024-01-02T00:00:00.000Z",
		})

		want := domainResourceModel{
			ID:             types.StringValue("abc"),
			Name:           types.StringValue("Research"),
			Description:    types.StringValue("Research domain"),
			Type:           types.StringValue("domain"),
			AssignmentType: types.StringValue("manual"),
			Autoscanning:   types.BoolValue(true),
			CreatedBy:      types.Int64Value(2),
			CreatedAt:      types.StringValue("2024-01-01T00:00:00.000Z"),
			UpdatedAt:      types.StringValue("2024-01-02T00:00:00.000Z"),
		}

		if model != want {
			t.Errorf("applyDomain produced\n  %+v\nwant\n  %+v", model, want)
		}
	})

	t.Run("omitted settings keep a known autoscanning value", func(t *testing.T) {
		t.Parallel()

		// A configured value is not contradicted by a response without
		// settings.
		model := domainResourceModel{Autoscanning: types.BoolValue(true)}
		model.applyDomain(&immutaapi.Domain{ID: "abc", Name: "Research"})

		if model.Autoscanning != types.BoolValue(true) {
			t.Errorf("autoscanning = %s, want the known value kept", model.Autoscanning)
		}

		// An unknown value is resolved to null.
		model = domainResourceModel{Autoscanning: types.BoolUnknown()}
		model.applyDomain(&immutaapi.Domain{ID: "abc", Name: "Research"})

		if !model.Autoscanning.IsNull() {
			t.Errorf("autoscanning = %s, want null", model.Autoscanning)
		}
	})

	t.Run("a sparse domain maps omitted values to null", func(t *testing.T) {
		t.Parallel()

		model := domainResourceModel{
			Description: types.StringNull(),
			Type:        types.StringValue("domain"),
		}
		model.applyDomain(&immutaapi.Domain{ID: "abc", Name: "Research"})

		want := domainResourceModel{
			ID:             types.StringValue("abc"),
			Name:           types.StringValue("Research"),
			Description:    types.StringNull(),
			Type:           types.StringValue("domain"),
			AssignmentType: types.StringNull(),
			Autoscanning:   types.BoolNull(),
			CreatedBy:      types.Int64Null(),
			CreatedAt:      types.StringNull(),
			UpdatedAt:      types.StringNull(),
		}

		if model != want {
			t.Errorf("applyDomain produced\n  %+v\nwant\n  %+v", model, want)
		}
	})
}

func TestAPIErrorDiagnostic(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err        error
		wantDetail string
	}{
		"unauthorized mentions permissions": {
			err:        &immutaapi.APIError{StatusCode: http.StatusUnauthorized, Method: http.MethodGet, Path: "/domain"},
			wantDetail: "GOVERNANCE permission",
		},
		"forbidden mentions permissions": {
			err:        &immutaapi.APIError{StatusCode: http.StatusForbidden, Method: http.MethodGet, Path: "/domain"},
			wantDetail: "GOVERNANCE permission",
		},
		"bad request mentions the attribute values": {
			err:        &immutaapi.APIError{StatusCode: http.StatusBadRequest, Method: http.MethodPost, Path: "/domain"},
			wantDetail: "attribute values",
		},
		"conflict mentions the possible causes": {
			err:        &immutaapi.APIError{StatusCode: http.StatusConflict, Method: http.MethodDelete, Path: "/domain/abc"},
			wantDetail: "already exist",
		},
		"transport errors are passed through": {
			err:        errors.New("connection refused"),
			wantDetail: "connection refused",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			diagnostic := apiErrorDiagnostic("Unable to do the thing", test.err)

			if diagnostic.Summary() != "Unable to do the thing" {
				t.Errorf("summary = %q", diagnostic.Summary())
			}

			if !strings.Contains(diagnostic.Detail(), test.wantDetail) {
				t.Errorf("detail = %q, want it to mention %q", diagnostic.Detail(), test.wantDetail)
			}
		})
	}
}

func TestDomainResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_domain" "test" {
  name        = "Research"
  description = "Research domain"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(domainResourceName, "id"),
					resource.TestCheckResourceAttr(domainResourceName, "name", "Research"),
					resource.TestCheckResourceAttr(domainResourceName, "description", "Research domain"),
					resource.TestCheckResourceAttr(domainResourceName, "type", "domain"),
					resource.TestCheckResourceAttr(domainResourceName, "assignment_type", "manual"),
					resource.TestCheckResourceAttr(domainResourceName, "autoscanning", "false"),
					resource.TestCheckResourceAttrSet(domainResourceName, "created_at"),
					// Omitted force_destroy stays null, like state written before
					// the attribute existed, so neither plans a change.
					resource.TestCheckNoResourceAttr(domainResourceName, "force_destroy"),
					checkDomainParity(server),
				),
			},
			{
				// Applying the same configuration twice changes nothing: a
				// PlanOnly step fails when the plan is not empty.
				Config: providerConfig(server.URL()) + `
resource "immuta_domain" "test" {
  name        = "Research"
  description = "Research domain"
}
`,
				PlanOnly: true,
			},
			{
				ResourceName:      domainResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Updating the description reuses the existing domain.
				Config: providerConfig(server.URL()) + `
resource "immuta_domain" "test" {
  name        = "Research"
  description = "Updated description"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "description", "Updated description"),
					checkDomainParity(server),
					checkDomainCount(server, 1),
				),
			},
			{
				// Renaming updates in place rather than replacing.
				Config: providerConfig(server.URL()) + `
resource "immuta_domain" "test" {
  name        = "Research renamed"
  description = "Updated description"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "name", "Research renamed"),
					checkDomainParity(server),
					checkDomainCount(server, 1),
				),
			},
			{
				// Removing the optional description clears it remotely and
				// leaves a null value in state rather than an empty string.
				Config: providerConfig(server.URL()) + `
resource "immuta_domain" "test" {
  name = "Research renamed"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(domainResourceName, "description"),
					checkDomainParity(server),
				),
			},
		},
	})
}

func TestDomainResourceDetectsRemoteDrift(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	var domainID string

	config := providerConfig(server.URL()) + `
resource "immuta_domain" "test" {
  name        = "Research"
  description = "Research domain"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  captureDomainID(&domainID),
			},
			{
				// A change made outside Terraform must show up as a diff.
				PreConfig: func() {
					server.SetDescription(domainID, "Changed in the Immuta UI")
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying the same configuration repairs the drift.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "description", "Research domain"),
					checkDomainParity(server),
				),
			},
		},
	})
}

func TestDomainResourceHandlesRemoteDeletion(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	var domainID string

	config := providerConfig(server.URL()) + `
resource "immuta_domain" "test" {
  name = "Research"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  captureDomainID(&domainID),
			},
			{
				// A domain deleted outside Terraform is removed from state
				// and planned for recreation.
				PreConfig: func() {
					server.Remove(domainID)
				},
				// A refresh-only step reuses the configuration of the
				// previous step, so Config must not be set here.
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[domainResourceName]; ok {
						return errors.New("the deleted domain should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkDomainCount(server, 1),
					checkDomainParity(server),
				),
			},
		},
	})
}

func TestDomainResourceRejectsInvalidConfiguration(t *testing.T) {
	skipWithoutTerraformCLI(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	tests := map[string]struct {
		body        string
		expectError *regexp.Regexp
	}{
		"an undocumented type is rejected": {
			body: `
resource "immuta_domain" "test" {
  name = "Research"
  type = "data_product"
}
`,
			expectError: regexp.MustCompile(`Attribute type value must be one of`),
		},
		"an empty name is rejected": {
			body: `
resource "immuta_domain" "test" {
  name = ""
}
`,
			expectError: regexp.MustCompile(`Attribute name string length must be at least 1`),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			clearProviderEnvironment(t)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      providerConfig(server.URL()) + test.body,
						PlanOnly:    true,
						ExpectError: test.expectError,
					},
				},
			})
		})
	}
}

func TestDomainResourceSurfacesAPIErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// The fake API rejects a duplicate name with a conflict, the
				// same way Immuta does.
				Config: providerConfig(server.URL()) + `
resource "immuta_domain" "first" {
  name = "Research"
}

resource "immuta_domain" "second" {
  name       = "Research"
  depends_on = [immuta_domain.first]
}
`,
				ExpectError: regexp.MustCompile(`Unable to create the Immuta domain`),
			},
		},
	})
}

func captureDomainID(target *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[domainResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", domainResourceName)
		}

		*target = resourceState.Primary.ID

		return nil
	}
}

// checkDomainParity asserts that Terraform state matches what the API stored.
func checkDomainParity(server *testutil.DomainServer) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[domainResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", domainResourceName)
		}

		client, err := immutaapi.NewClient(server.URL(), testutil.TestAPIKey)
		if err != nil {
			return fmt.Errorf("building an API client: %w", err)
		}

		domain, err := client.GetDomain(context.Background(), resourceState.Primary.ID)
		if err != nil {
			return fmt.Errorf("reading the domain from the API: %w", err)
		}

		return testutil.DomainParityError(domain, resourceState.Primary.Attributes)
	}
}

func checkDomainCount(server *testutil.DomainServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Count(); got != want {
			return fmt.Errorf("the API holds %d domains, want %d", got, want)
		}

		return nil
	}
}

func checkAllDomainsDestroyed(server *testutil.DomainServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Count(); got != 0 {
			return fmt.Errorf("%d domains are left in the API after destroy", got)
		}

		return nil
	}
}

func domainAutoscanningConfig(serverURL, attributes string) string {
	return providerConfig(serverURL) + `
resource "immuta_domain" "test" {
  name = "Research"
` + attributes + `}
`
}

// checkSettingsRequests checks how many documented settings updates were
// sent.
func checkSettingsRequests(server *testutil.DomainServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got := 0
		for _, request := range server.Requests() {
			if request.Method == http.MethodPut && strings.HasSuffix(request.Path, "/settings") {
				got++
			}
		}
		if got != want {
			return fmt.Errorf("PUT /domain/{domainId}/settings requests = %d, want %d", got, want)
		}

		return nil
	}
}

func checkServerAutoscanning(server *testutil.DomainServer, domainID *string, want bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, ok := server.Autoscanning(*domainID)
		if !ok || got != want {
			return fmt.Errorf("autoscanning in Immuta = %t (found %t), want %t", got, ok, want)
		}

		return nil
	}
}

func TestDomainResourceAutoscanning(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	var domainID string
	enabled := domainAutoscanningConfig(server.URL(), "  autoscanning = true\n")
	disabled := domainAutoscanningConfig(server.URL(), "  autoscanning = false\n")
	omitted := domainAutoscanningConfig(server.URL(), "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				// Create sets the configured value with the settings endpoint.
				Config: enabled,
				Check: resource.ComposeAggregateTestCheckFunc(
					captureDomainID(&domainID),
					resource.TestCheckResourceAttr(domainResourceName, "autoscanning", "true"),
					checkServerAutoscanning(server, &domainID, true),
					checkSettingsRequests(server, 1),
				),
			},
			{
				Config:   enabled,
				PlanOnly: true,
			},
			{
				ResourceName:      domainResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Changing it updates in place.
				Config: disabled,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "autoscanning", "false"),
					checkServerAutoscanning(server, &domainID, false),
					checkSettingsRequests(server, 2),
					checkDomainCount(server, 1),
				),
			},
			{
				// Changing another attribute does not send the settings again.
				Config: domainAutoscanningConfig(server.URL(), "  description  = \"Research domain\"\n  autoscanning = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "description", "Research domain"),
					checkSettingsRequests(server, 2),
				),
			},
			{
				// A change made in the Immuta UI shows up as drift and is put back.
				PreConfig: func() { server.SetAutoscanning(domainID, true) },
				Config:    disabled,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "autoscanning", "false"),
					checkServerAutoscanning(server, &domainID, false),
					checkSettingsRequests(server, 3),
				),
			},
			{
				// Removing it from the configuration changes nothing.
				Config:   omitted,
				PlanOnly: true,
			},
			{
				// Once omitted, a change in the Immuta UI is reported, not reverted.
				PreConfig: func() { server.SetAutoscanning(domainID, true) },
				Config:    omitted,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainResourceName, "autoscanning", "true"),
					checkServerAutoscanning(server, &domainID, true),
					checkSettingsRequests(server, 3),
				),
			},
		},
	})
}

func TestDomainResourceLeavesAutoscanningAloneWhenOmitted(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: domainAutoscanningConfig(server.URL(), ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Immuta's value is reported, and no settings are sent.
					resource.TestCheckResourceAttr(domainResourceName, "autoscanning", "false"),
					checkSettingsRequests(server, 0),
				),
			},
		},
	})
}

// TestDomainResourceTracksTheDomainWhenSettingsFail checks that a domain is
// kept in state, and so destroyed, when its settings fail to apply after it
// was created.
func TestDomainResourceTracksTheDomainWhenSettingsFail(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/settings") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"missing permission"}`))

			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		// Destroying the tainted domain leaves nothing behind.
		CheckDestroy: checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config:      domainAutoscanningConfig(front.URL, "  autoscanning = true\n"),
				ExpectError: regexp.MustCompile(`unable to set its autoscanning setting`),
			},
			{
				// The domain was saved to state, tainted, so it is replaced.
				Config:             domainAutoscanningConfig(front.URL, "  autoscanning = true\n"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})

	// CheckDestroy is skipped when state is empty, so check directly that the
	// created domain was tracked and destroyed rather than orphaned.
	if got := server.Count(); got != 0 {
		t.Errorf("%d domains are left in the API: the created domain was not saved to state", got)
	}
}

// forceDestroyDomainConfig is immuta_domain.test, with the force_destroy line
// given, and data sources assigned to it dynamically by the Sales tag: the
// data sources 2 and 3 of tagDataSources.
func forceDestroyDomainConfig(serverURL, forceDestroy string) string {
	return providerConfig(serverURL) + fmt.Sprintf(`
resource "immuta_domain" "test" {
  name = "Denmark"
  %s
}

resource "immuta_domain_dynamic_assignment" "test" {
  domain_id = immuta_domain.test.id
  tags      = ["Sales"]
}
`, forceDestroy)
}

// checkCollectionRemovals checks which data sources were removed with the
// DELETE /collection/{domainId}/datasources/{dataSourceId} the UI uses.
func checkCollectionRemovals(t *testing.T, server *testutil.DomainServer, want ...string) {
	t.Helper()

	var got []string
	for _, request := range server.Requests() {
		if request.Method == http.MethodDelete && strings.HasPrefix(request.Path, "/collection/") {
			got = append(got, request.Path[strings.LastIndex(request.Path, "/")+1:])
		}
	}
	if !sameStrings(got, want) {
		t.Errorf("data sources removed through /collection = %q, want %q", got, want)
	}
}

// With force_destroy, destroying the domain removes the data sources a
// dynamic assignment left in it, without deleting them, then the domain.
func TestDomainResourceForceDestroyRemovesDataSources(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainAssignmentPolling(t, time.Minute)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)
	tagDataSources(server)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: forceDestroyDomainConfig(server.URL(), ""),
			},
			{
				// Turning force_destroy on updates the state only.
				Config: forceDestroyDomainConfig(server.URL(), "force_destroy = true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(domainResourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr(domainResourceName, "force_destroy", "true"),
			},
			{
				// The assignment and the domain are destroyed in one apply.
				Config: providerConfig(server.URL()),
			},
		},
	})

	checkCollectionRemovals(t, server, "2", "3")
	for _, id := range []int{2, 3} {
		if domain := server.DataSourceDomain(id); domain != "" {
			t.Errorf("data source %d is still in the domain %s", id, domain)
		}
	}
}

// Without force_destroy, set to false or omitted, nothing is removed and
// Immuta's refusal is reported, pointing at force_destroy.
func TestDomainResourceWithoutForceDestroyKeepsDataSources(t *testing.T) {
	for name, forceDestroy := range map[string]string{"false": "force_destroy = false", "omitted": ""} {
		t.Run(name, func(t *testing.T) {
			skipWithoutTerraformCLI(t)
			clearProviderEnvironment(t)
			fastDomainAssignmentPolling(t, time.Minute)

			server := testutil.NewDomainServer()
			t.Cleanup(server.Close)
			tagDataSources(server)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				CheckDestroy:             checkAllDomainsDestroyed(server),
				Steps: []resource.TestStep{
					{
						Config: forceDestroyDomainConfig(server.URL(), forceDestroy),
					},
					{
						Config: providerConfig(server.URL()),
						ExpectError: regexp.MustCompile(`2\s+data\s+sources\s+\(dataSourceCount\)[\s\S]*` +
							`set\s+force_destroy\s+=\s+true\s+on\s+the\s+immuta_domain`),
					},
					{
						PreConfig: func() {
							checkCollectionRemovals(t, server)
							server.RemoveDataSourcesFromDomains()
						},
						Config: providerConfig(server.URL()),
					},
				},
			})
		})
	}
}

// A data source that left the domain while force_destroy empties it, so that
// removing it answers 404, does not stop the delete.
func TestDomainResourceForceDestroyIgnoresDataSourcesAlreadyRemoved(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainAssignmentPolling(t, time.Minute)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)
	tagDataSources(server)

	target, err := url.Parse(server.URL())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/collection/") && strings.HasSuffix(r.URL.Path, "/2") {
			server.SetDataSourceDomain(2, "")
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: forceDestroyDomainConfig(front.URL, "force_destroy = true"),
			},
			{
				Config: providerConfig(front.URL),
			},
		},
	})

	if got := server.Count(); got != 0 {
		t.Errorf("%d domains are left in the API", got)
	}
}

// When removing a data source fails partway, the domain is not deleted, the
// diagnostic says what was removed, and applying again finishes the job.
func TestDomainResourceForceDestroyStopsOnFailure(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainAssignmentPolling(t, time.Minute)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)
	tagDataSources(server)

	target, err := url.Parse(server.URL())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	var failing atomic.Bool
	failing.Store(true)
	proxy := httputil.NewSingleHostReverseProxy(target)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() && r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/collection/") &&
			strings.HasSuffix(r.URL.Path, "/3") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal error"}`))

			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: forceDestroyDomainConfig(front.URL, "force_destroy = true"),
			},
			{
				Config: providerConfig(front.URL),
				ExpectError: regexp.MustCompile(`Unable\s+to\s+remove\s+the\s+data\s+source\s+3\s+from\s+the\s+Immuta\s+domain` +
					`[\s\S]*removed\s+1\s+of\s+the\s+2\s+data\s+sources\s+\["2"\][\s\S]*domain\s+was\s+not\s+deleted`),
			},
			{
				PreConfig: func() {
					if got := server.Count(); got != 1 {
						t.Errorf("%d domains in the API after the failure, want 1", got)
					}
					if got := server.DataSourceDomain(2); got != "" {
						t.Errorf("data source 2 is still in the domain %s", got)
					}
					if got := server.DataSourceDomain(3); got == "" {
						t.Error("data source 3 left the domain although removing it failed")
					}
					failing.Store(false)
				},
				Config: providerConfig(front.URL),
			},
		},
	})

	if got := server.Count(); got != 0 {
		t.Errorf("%d domains are left in the API", got)
	}
}
