package provider

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const testDomainConfig = `
resource "immuta_domain" "test" {
  name = "Denmark"
}
`

func domainDynamicAssignmentConfig(tags string) string {
	return testDomainConfig + fmt.Sprintf(`
resource "immuta_domain_dynamic_assignment" "test" {
  domain_id = immuta_domain.test.id
  tags      = %s
}
`, tags)
}

// domainID returns the ID of immuta_domain.test from state.
func domainID(state *terraform.State) string {
	return state.RootModule().Resources["immuta_domain.test"].Primary.ID
}

func checkDomainAssignment(server *testutil.DomainServer, wantType string, wantTags ...string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		gotType, gotTags := server.Assignment(domainID(state))
		if gotType != wantType || !sameStrings(gotTags, wantTags) {
			return fmt.Errorf("assignment = %s %v, want %s %v", gotType, gotTags, wantType, wantTags)
		}

		return nil
	}
}

func TestDomainDynamicAssignmentResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	provider := providerConfig(server.URL())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: provider + domainDynamicAssignmentConfig(`["Discovered.Country.Denmark", "Sales"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("immuta_domain_dynamic_assignment.test", "id", "immuta_domain.test", "id"),
					resource.TestCheckResourceAttr("immuta_domain_dynamic_assignment.test", "tags.#", "2"),
					resource.TestCheckTypeSetElemAttr("immuta_domain_dynamic_assignment.test", "tags.*", "Discovered.Country.Denmark"),
					checkDomainAssignment(server, "dynamic", "Discovered.Country.Denmark", "Sales"),
				),
			},
			{
				// Neither resource sees a change: the tag order Immuta returns
				// is not one, and immuta_domain only reads assignment_type.
				Config:   provider + domainDynamicAssignmentConfig(`["Sales", "Discovered.Country.Denmark"]`),
				PlanOnly: true,
			},
			{
				Config: provider + domainDynamicAssignmentConfig(`["Discovered.Country.Denmark"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_domain_dynamic_assignment.test", "tags.#", "1"),
					resource.TestCheckResourceAttr("immuta_domain.test", "assignment_type", "dynamic"),
					checkDomainAssignment(server, "dynamic", "Discovered.Country.Denmark"),
					checkDomainCount(server, 1),
				),
			},
			{
				ResourceName:      "immuta_domain_dynamic_assignment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Removing the assignment sets the domain back to manual and
				// keeps the domain.
				Config: provider + testDomainConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkDomainAssignment(server, "manual"),
					checkDomainCount(server, 1),
				),
			},
		},
	})
}

func TestDomainDynamicAssignmentResourceDrift(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + domainDynamicAssignmentConfig(`["Discovered.Country.Denmark"]`)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(state *terraform.State) error {
					id = domainID(state)

					return nil
				},
			},
			{
				// Switched to manual in Immuta.
				PreConfig:          func() { server.SetAssignment(id, "manual") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check:  checkDomainAssignment(server, "dynamic", "Discovered.Country.Denmark"),
			},
			{
				// Tags changed in Immuta.
				PreConfig:          func() { server.SetAssignment(id, "dynamic", "Discovered.Country.Sweden") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check:  checkDomainAssignment(server, "dynamic", "Discovered.Country.Denmark"),
			},
			{
				// The domain deleted in Immuta: both leave state.
				PreConfig:          func() { server.Remove(id) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources["immuta_domain_dynamic_assignment.test"]; ok {
						return errors.New("the assignment of the deleted domain should have been removed from state")
					}

					return nil
				},
			},
		},
	})
}

func TestDomainDynamicAssignmentResourceValidation(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	for name, tc := range map[string]struct {
		tags string
		want string
	}{
		"no tags":    {`[]`, `at\s+least\s+1`},
		"empty name": {`[""]`, `length\s+must\s+be\s+at\s+least\s+1`},
	} {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{{
					Config: providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_domain_dynamic_assignment" "test" {
  domain_id = "c7458ee9-03e5-4b6f-8395-ad4e49235428"
  tags      = %s
}
`, tc.tags),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
	if got := len(server.Requests()); got != 0 {
		t.Errorf("the API received %d requests, want none for an invalid configuration", got)
	}
}

// fastDomainAssignmentPolling makes the provider poll quickly for a dynamic
// assignment and give up after timeout.
func fastDomainAssignmentPolling(t *testing.T, timeout time.Duration) {
	t.Helper()

	interval, wait := domainAssignmentPollInterval, domainAssignmentTimeout
	domainAssignmentPollInterval, domainAssignmentTimeout = 10*time.Millisecond, timeout
	t.Cleanup(func() { domainAssignmentPollInterval, domainAssignmentTimeout = interval, wait })
}

// Immuta applies a dynamic assignment in the background, so GET /domain/{id}
// returns the previous assignment for a while after the request.
func TestDomainDynamicAssignmentResourceWaitsForImmuta(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainAssignmentPolling(t, time.Minute)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)
	server.SetAssignmentDelay(2)

	provider := providerConfig(server.URL())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: provider + domainDynamicAssignmentConfig(`["Discovered.Country.Denmark", "Sales"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_domain_dynamic_assignment.test", "tags.#", "2"),
					resource.TestCheckTypeSetElemAttr("immuta_domain_dynamic_assignment.test", "tags.*", "Discovered.Country.Denmark"),
					resource.TestCheckTypeSetElemAttr("immuta_domain_dynamic_assignment.test", "tags.*", "Sales"),
					checkDomainAssignment(server, "dynamic", "Discovered.Country.Denmark", "Sales"),
				),
			},
			{
				Config: provider + domainDynamicAssignmentConfig(`["Sales"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_domain_dynamic_assignment.test", "tags.#", "1"),
					resource.TestCheckTypeSetElemAttr("immuta_domain_dynamic_assignment.test", "tags.*", "Sales"),
					checkDomainAssignment(server, "dynamic", "Sales"),
				),
			},
		},
	})
}

func TestDomainDynamicAssignmentResourceTimesOut(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainAssignmentPolling(t, 100*time.Millisecond)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)
	server.SetAssignmentDelay(1_000_000)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config:      providerConfig(server.URL()) + domainDynamicAssignmentConfig(`["Sales"]`),
				ExpectError: regexp.MustCompile(`Timed out waiting for Immuta to assign data sources`),
			},
		},
	})
}

// tagDataSources stores data sources tagged for the dynamic assignment
// tests, in no domain yet.
func tagDataSources(server *testutil.DomainServer) {
	for _, dataSource := range []testutil.DataSource{
		{ID: 1, Name: "Danish customers", Tags: []string{"Discovered.Country.Denmark"}},
		{ID: 2, Name: "Danish orders", Tags: []string{"Discovered.Country.Denmark", "Sales"}},
		{ID: 3, Name: "Swedish orders", Tags: []string{"Sales"}},
		{ID: 4, Name: "HR people", Tags: []string{"HR"}},
	} {
		server.AddDataSource(dataSource)
	}
}

// domainDataSourcesConfig reads the data sources of the domain once the
// assignment is applied, the way a data product built from them does.
func domainDataSourcesConfig(tags string) string {
	return domainDynamicAssignmentConfig(tags) + `
data "immuta_domain_data_sources" "test" {
  domain_id = immuta_domain_dynamic_assignment.test.domain_id
}
`
}

// Immuta adds the data sources of a dynamic assignment in the background, so
// GET /domain/{id}/datasources misses them for a while after the tags show.
// Waiting for them lets a fresh apply read them without a second apply.
func TestDomainDynamicAssignmentResourceWaitsForDataSources(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainAssignmentPolling(t, time.Minute)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)
	tagDataSources(server)
	server.SetAssignmentDelay(1)
	server.DelayDomainDataSourceJobs(3)

	provider := providerConfig(server.URL())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: provider + domainDataSourcesConfig(`["Discovered.Country.Denmark"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.test", "ids.#", "2"),
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.test", "ids.0", "1"),
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.test", "ids.1", "2"),
				),
			},
			{
				// Two kept, one added.
				Config: provider + domainDataSourcesConfig(`["Discovered.Country.Denmark", "Sales"]`),
				Check:  resource.TestCheckResourceAttr("data.immuta_domain_data_sources.test", "ids.#", "3"),
			},
			{
				// One kept, two removed.
				Config: provider + domainDataSourcesConfig(`["HR"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.test", "ids.#", "1"),
					resource.TestCheckResourceAttr("data.immuta_domain_data_sources.test", "ids.0", "4"),
				),
			},
		},
	})
}

func TestDomainDynamicAssignmentResourceTimesOutWaitingForDataSources(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainAssignmentPolling(t, 100*time.Millisecond)

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)
	tagDataSources(server)
	server.DelayDomainDataSourceJobs(1_000_000)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllDomainsDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config:      providerConfig(server.URL()) + domainDynamicAssignmentConfig(`["Sales"]`),
				ExpectError: regexp.MustCompile(`reported\s+2\s+data\s+sources\s+in\s+the\s+domain,\s+but\s+after\s+\S+\s+the\s+domain\s+lists\s+0`),
			},
		},
	})
}
