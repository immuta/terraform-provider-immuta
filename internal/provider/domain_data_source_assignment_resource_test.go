package provider

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

// fastDomainDataSourcePolling shortens the wait for Immuta's background job.
// Tests using it must not run in parallel.
func fastDomainDataSourcePolling(t *testing.T, timeout time.Duration) {
	t.Helper()

	interval, add := domainDataSourcePollInterval, domainDataSourceAddTimeout
	domainDataSourcePollInterval, domainDataSourceAddTimeout = 10*time.Millisecond, timeout
	t.Cleanup(func() { domainDataSourcePollInterval, domainDataSourceAddTimeout = interval, add })
}

// newDataSourceServer is a domain server holding three Snowflake tables.
func newDataSourceServer(t *testing.T) *testutil.DomainServer {
	t.Helper()

	server := testutil.NewDomainServer()
	t.Cleanup(server.Close)

	for _, dataSource := range []testutil.DataSource{
		{ID: 36, Name: "acme . acme_dev . sales . orders", ConnectionKey: "acme", NamePath: []string{"acme_dev", "sales", "orders"}},
		{ID: 37, Name: "acme . acme_dev . sales . customers", ConnectionKey: "acme", NamePath: []string{"acme_dev", "sales", "customers"}},
		// The same table name elsewhere, which must not be picked.
		{ID: 38, Name: "acme . acme_prod . sales . orders", ConnectionKey: "acme", NamePath: []string{"acme_prod", "sales", "orders"}},
		{ID: 39, Name: "other . acme_dev . sales . orders", ConnectionKey: "other", NamePath: []string{"acme_dev", "sales", "orders"}},
	} {
		server.AddDataSource(dataSource)
	}

	return server
}

const domainDataSourceAssignmentConfig = testDomainConfig + `
data "immuta_data_source" "orders" {
  connection_key = "acme"
  path           = ["acme_dev", "sales", "orders"]
}

data "immuta_data_source" "customers" {
  name = "acme . acme_dev . sales . customers"
}

resource "immuta_domain_data_source_assignment" "orders" {
  domain_id      = immuta_domain.test.id
  data_source_id = data.immuta_data_source.orders.id
}
`

const customersAssignmentConfig = `
resource "immuta_domain_data_source_assignment" "customers" {
  domain_id      = immuta_domain.test.id
  data_source_id = data.immuta_data_source.customers.id
}
`

func checkDataSourceDomain(server *testutil.DomainServer, id int, inDomain bool) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		want := ""
		if inDomain {
			want = domainID(state)
		}
		if got := server.DataSourceDomain(id); got != want {
			return fmt.Errorf("domain of data source %d = %q, want %q", id, got, want)
		}

		return nil
	}
}

func TestDomainDataSourceAssignmentResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainDataSourcePolling(t, 5*time.Second)

	server := newDataSourceServer(t)
	// Immuta's background job has not run for the first checks.
	server.DelayDomainDataSourceJobs(2)

	provider := providerConfig(server.URL())

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy: func(*terraform.State) error {
			for _, id := range []int{36, 37} {
				if got := server.DataSourceDomain(id); got != "" {
					return fmt.Errorf("data source %d is still in the domain %s after destroy", id, got)
				}
			}

			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: provider + domainDataSourceAssignmentConfig + customersAssignmentConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.immuta_data_source.orders", "id", "36"),
					resource.TestCheckResourceAttr("data.immuta_data_source.orders", "name", "acme . acme_dev . sales . orders"),
					resource.TestCheckResourceAttr("data.immuta_data_source.orders", "data_object_id", "4eed11f5-1368-4b75-afda-000000000036"),
					resource.TestCheckResourceAttr("data.immuta_data_source.customers", "id", "37"),
					resource.TestCheckResourceAttr("data.immuta_data_source.customers", "connection_key", "acme"),
					resource.TestCheckResourceAttr("data.immuta_data_source.customers", "path.2", "customers"),
					resource.TestCheckResourceAttr("immuta_domain_data_source_assignment.orders", "data_source_id", "36"),
					func(state *terraform.State) error {
						want := domainID(state) + "/36"
						if got := state.RootModule().Resources["immuta_domain_data_source_assignment.orders"].Primary.ID; got != want {
							return fmt.Errorf("id = %s, want %s", got, want)
						}

						return nil
					},
					checkDataSourceDomain(server, 36, true),
					checkDataSourceDomain(server, 37, true),
					checkDataSourceDomain(server, 38, false),
				),
			},
			{
				Config:   provider + domainDataSourceAssignmentConfig + customersAssignmentConfig,
				PlanOnly: true,
			},
			{
				ResourceName:      "immuta_domain_data_source_assignment.orders",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Removing one assignment removes only that data source.
				Config: provider + domainDataSourceAssignmentConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkDataSourceDomain(server, 36, true),
					checkDataSourceDomain(server, 37, false),
				),
			},
		},
	})
}

func TestDomainDataSourceAssignmentResourceDrift(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainDataSourcePolling(t, 5*time.Second)

	server := newDataSourceServer(t)
	config := providerConfig(server.URL()) + domainDataSourceAssignmentConfig

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// Removed from the domain in Immuta: gone from state.
				PreConfig:          func() { server.SetDataSourceDomain(36, "") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources["immuta_domain_data_source_assignment.orders"]; ok {
						return errors.New("the removed assignment should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check:  checkDataSourceDomain(server, 36, true),
			},
		},
	})
}

func TestDomainDataSourceAssignmentResourceErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)
	fastDomainDataSourcePolling(t, 200*time.Millisecond)

	t.Run("already in the domain", func(t *testing.T) {
		server := newDataSourceServer(t)
		provider := providerConfig(server.URL())
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProviderFactories(),
			Steps: []resource.TestStep{
				{
					Config: provider + testDomainConfig,
					Check: func(state *terraform.State) error {
						server.SetDataSourceDomain(36, domainID(state))

						return nil
					},
				},
				{
					Config:      provider + domainDataSourceAssignmentConfig,
					ExpectError: regexp.MustCompile(`(?s)already\s+in\s+the\s+Immuta\s+domain.*terraform\s+import`),
				},
			},
		})
	})

	t.Run("dynamic domain", func(t *testing.T) {
		server := newDataSourceServer(t)
		dynamic := providerConfig(server.URL()) + testDomainConfig + `
resource "immuta_domain_dynamic_assignment" "test" {
  domain_id = immuta_domain.test.id
  tags      = ["Sales"]
}
`
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProviderFactories(),
			Steps: []resource.TestStep{
				// The domain is dynamic before the data source is added.
				{Config: dynamic},
				{
					Config:      dynamic + strings.TrimPrefix(domainDataSourceAssignmentConfig, testDomainConfig),
					ExpectError: regexp.MustCompile(`(?s)manual\s+assignment.*immuta_domain_dynamic_assignment`),
				},
			},
		})
	})

	t.Run("in another domain", func(t *testing.T) {
		server := newDataSourceServer(t)
		server.SetDataSourceDomain(36, "another-domain")
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProviderFactories(),
			Steps: []resource.TestStep{{
				Config:      providerConfig(server.URL()) + domainDataSourceAssignmentConfig,
				ExpectError: regexp.MustCompile(`(?s)already\s+assigned\s+to\s+a\s+domain.*another\s+domain`),
			}},
		})
	})

	t.Run("job never finishes", func(t *testing.T) {
		server := newDataSourceServer(t)
		server.DelayDomainDataSourceJobs(1_000_000)
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProviderFactories(),
			Steps: []resource.TestStep{{
				Config:      providerConfig(server.URL()) + domainDataSourceAssignmentConfig,
				ExpectError: regexp.MustCompile(`(?s)Timed\s+out.*background\s+job\s+e48e15a0`),
			}},
		})
	})

	t.Run("lookups", func(t *testing.T) {
		server := newDataSourceServer(t)
		provider := providerConfig(server.URL())
		for name, tc := range map[string]struct {
			config string
			want   string
		}{
			"path not found": {
				`data "immuta_data_source" "test" {
  connection_key = "acme"
  path           = ["acme_dev", "orders"]
}`,
				`(?s)No\s+Immuta\s+data\s+source.*acme_dev\.orders.*Paths\s+found\s+in\s+that\s+connection:\s+acme_dev\.sales\.orders,\s+acme_prod\.sales\.orders`,
			},
			"name not found": {
				`data "immuta_data_source" "test" {
  name = "acme . acme_dev . sales"
}`,
				`No\s+Immuta\s+data\s+source\s+you\s+can\s+see\s+is\s+named`,
			},
			"name and connection": {
				`data "immuta_data_source" "test" {
  name           = "orders"
  connection_key = "acme"
  path           = ["orders"]
}`,
				`Invalid\s+Attribute\s+Combination`,
			},
			"connection without path": {
				`data "immuta_data_source" "test" {
  connection_key = "acme"
}`,
				`(?s)Invalid\s+Attribute\s+Combination.*path`,
			},
			"non-numeric data source id": {
				`resource "immuta_domain_data_source_assignment" "test" {
  domain_id      = "d1"
  data_source_id = "orders"
}`,
				`must\s+be\s+a\s+data\s+source\s+ID`,
			},
		} {
			t.Run(name, func(t *testing.T) {
				resource.UnitTest(t, resource.TestCase{
					ProtoV6ProviderFactories: testProviderFactories(),
					Steps:                    []resource.TestStep{{Config: provider + tc.config, ExpectError: regexp.MustCompile(tc.want)}},
				})
			})
		}
	})

	t.Run("import id", func(t *testing.T) {
		server := newDataSourceServer(t)
		for _, id := range []string{"no-separator", "d1/orders", "/36"} {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{{
					Config: providerConfig(server.URL()) + `
resource "immuta_domain_data_source_assignment" "test" {
  domain_id      = "d1"
  data_source_id = "36"
}
`,
					ResourceName:  "immuta_domain_data_source_assignment.test",
					ImportState:   true,
					ImportStateId: id,
					ExpectError:   regexp.MustCompile(`Expected\s+<domain_id>/<data_source_id>`),
				}},
			})
		}
	})
}
