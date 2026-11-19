package provider

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const (
	tagResourceName              = "immuta_tag.test"
	identifierResourceName       = "immuta_identifier.test"
	domainIdentifierResourceName = "immuta_domain_identifier.test"
)

func TestTagResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + `
resource "immuta_tag" "test" {
  name = "Discovered.Primary color"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(tagResourceName, "id"),
					resource.TestCheckResourceAttr(tagResourceName, "name", "Discovered.Primary color"),
					resource.TestCheckResourceAttr(tagResourceName, "source", "curated"),
					resource.TestCheckResourceAttr(tagResourceName, "system_created", "false"),
					resource.TestCheckResourceAttr(tagResourceName, "delete_hierarchy", "false"),
					// Immuta creates the parent tag as well.
					checkTagNames(server, []string{"Discovered", "Discovered.Primary color"}),
				),
			},
			{
				// Applying the same configuration twice changes nothing.
				Config:   config,
				PlanOnly: true,
			},
			{
				// Tags are imported by their fully qualified name.
				ResourceName:                         tagResourceName,
				ImportState:                          true,
				ImportStateId:                        "Discovered.Primary color",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

// TestTagResourceAddsChildrenUnderAnExistingRoot reproduces a tenant where
// Immuta has already created the Discovered hierarchy. A plain create is
// rejected there, so the child must be added with the documented rootTag
// field, and the existing children of the root must survive.
func TestTagResourceAddsChildrenUnderAnExistingRoot(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)
	server.SeedTag("Discovered.Entity.Existing child")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_tag" "test" {
  name = "Discovered.Entity.Primary color"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(tagResourceName, "name", "Discovered.Entity.Primary color"),
					checkTagNames(server, []string{
						"Discovered",
						"Discovered.Entity",
						"Discovered.Entity.Existing child",
						"Discovered.Entity.Primary color",
					}),
					checkRootTagRequestsNeverDeleteTheHierarchy(server, 1),
				),
			},
		},
	})
}

// TestTagResourceCreatesSiblingsUnderANewRootInParallel creates four tags under
// a root that does not exist yet. Terraform creates them in parallel, so
// without serialisation both would send a plain create and the second would
// be rejected once the first had created the root.
func TestTagResourceCreatesSiblingsUnderANewRootInParallel(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_tag" "credit_card" {
  name = "Example.Credit card"
}

resource "immuta_tag" "primary_color" {
  name = "Example.Primary color"
}

resource "immuta_tag" "postcode" {
  name = "Example.Postcode"
}

resource "immuta_tag" "phone_number" {
  name = "Example.Phone number"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkTagNames(server, []string{
						"Example",
						"Example.Credit card",
						"Example.Phone number",
						"Example.Postcode",
						"Example.Primary color",
					}),
					// The first create made the root; the other three were
					// added under it.
					checkRootTagRequestsNeverDeleteTheHierarchy(server, 3),
				),
			},
		},
	})
}

// TestTagResourceCreatesNestedSiblingsUnderANewRoot mirrors what the Immuta UI
// sends when it creates Access.Layer.{Bronze,Silver,Gold} in one request. The
// provider creates one tag per resource instead: the first plain create makes
// the implicit parents Access and Access.Layer, and the other two are added
// under the Access root. Destroy removes only the managed leaves, leaving the
// implicit parents to Immuta.
func TestTagResourceCreatesNestedSiblingsUnderANewRoot(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkTagNames(server, []string{"Access", "Access.Layer"}),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_tag" "silver" {
  name = "Access.Layer.Silver"
}

resource "immuta_tag" "bronze" {
  name = "Access.Layer.Bronze"
}

resource "immuta_tag" "gold" {
  name = "Access.Layer.Gold"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("immuta_tag.silver", "name", "Access.Layer.Silver"),
					resource.TestCheckResourceAttr("immuta_tag.bronze", "name", "Access.Layer.Bronze"),
					resource.TestCheckResourceAttr("immuta_tag.gold", "name", "Access.Layer.Gold"),
					checkTagNames(server, []string{
						"Access",
						"Access.Layer",
						"Access.Layer.Bronze",
						"Access.Layer.Gold",
						"Access.Layer.Silver",
					}),
					// The first create made the root; the other two were added
					// under it.
					checkRootTagRequestsNeverDeleteTheHierarchy(server, 2),
				),
			},
		},
	})
}

func TestTagResourceHandlesRemoteDeletion(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + `
resource "immuta_tag" "test" {
  name = "Discovered.Primary color"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					server.RemoveTag("Discovered.Primary color")
				},
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[tagResourceName]; ok {
						return errors.New("the deleted tag should have been removed from state")
					}

					return nil
				},
			},
			{Config: config},
		},
	})
}

func TestIdentifierResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)
	server.AddDomain("domain-1", "Finance")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllIdentifiersDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: identifierConfig(server.URL(), "Primary colors", "Columns that hold a primary color", `"(red|green|blue)"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(identifierResourceName, "id"),
					resource.TestCheckResourceAttr(identifierResourceName, "name", "Primary colors"),
					resource.TestCheckResourceAttr(identifierResourceName, "type", "regex"),
					resource.TestCheckResourceAttr(identifierResourceName, "regex", "(red|green|blue)"),
					resource.TestCheckResourceAttr(identifierResourceName, "domain_id", "domain-1"),
					resource.TestCheckResourceAttr(identifierResourceName, "tags.#", "1"),
					resource.TestCheckTypeSetElemAttr(identifierResourceName, "tags.*", "Discovered.Primary color"),
					resource.TestCheckNoResourceAttr(identifierResourceName, "values"),
					resource.TestCheckResourceAttrSet(identifierResourceName, "created_at"),
				),
			},
			{
				// Applying the same configuration twice changes nothing.
				Config:   identifierConfig(server.URL(), "Primary colors", "Columns that hold a primary color", `"(red|green|blue)"`),
				PlanOnly: true,
			},
			{
				ResourceName:      identifierResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// The documented update replaces the whole identifier, and is
				// applied in place rather than recreating it.
				Config: identifierConfig(server.URL(), "Primary colours", "Updated description", `"(red|green|blue|yellow)"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(identifierResourceName, "name", "Primary colours"),
					resource.TestCheckResourceAttr(identifierResourceName, "description", "Updated description"),
					resource.TestCheckResourceAttr(identifierResourceName, "regex", "(red|green|blue|yellow)"),
					checkIdentifierCount(server, 1),
				),
			},
		},
	})
}

func TestIdentifierResourceSupportsDictionaries(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllIdentifiersDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + `
resource "immuta_tag" "test" {
  name = "Discovered.Primary color"
}

resource "immuta_identifier" "test" {
  name           = "Primary colors"
  description    = "Columns that hold a primary color"
  type           = "dictionary"
  values         = ["red", "green", "blue"]
  case_sensitive = true
  tags           = [immuta_tag.test.name]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(identifierResourceName, "type", "dictionary"),
					resource.TestCheckResourceAttr(identifierResourceName, "values.#", "3"),
					resource.TestCheckResourceAttr(identifierResourceName, "case_sensitive", "true"),
					resource.TestCheckNoResourceAttr(identifierResourceName, "regex"),
				),
			},
		},
	})
}

func TestIdentifierResourceValidatesTypeAgainstConfig(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)

	tests := map[string]struct {
		body string
		want *regexp.Regexp
	}{
		"a regex identifier without a regex": {
			body: `type = "regex"`,
			want: regexp.MustCompile(`Missing regex`),
		},
		"a dictionary identifier without values": {
			body: `type = "dictionary"`,
			want: regexp.MustCompile(`Missing values`),
		},
		"a regex identifier with dictionary values": {
			body: "type = \"regex\"\n  regex = \"x\"\n  values = [\"red\"]",
			want: regexp.MustCompile(`Unexpected values`),
		},
		"a dictionary identifier with a regex": {
			body: "type = \"dictionary\"\n  values = [\"red\"]\n  regex = \"x\"",
			want: regexp.MustCompile(`Unexpected regex`),
		},
		"an unknown type": {
			body: `type = "fuzzy"`,
			want: regexp.MustCompile(`Attribute type value must be one of`),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_identifier" "test" {
  name        = "Primary colors"
  description = "Columns that hold a primary color"
  tags        = ["Discovered.Primary color"]
  %s
}
`, test.body),
						PlanOnly:    true,
						ExpectError: test.want,
					},
				},
			})
		})
	}
}

func TestIdentifierResourceExplainsAMissingTag(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Immuta rejects an identifier that references a tag which does
				// not exist. The diagnostic has to point at immuta_tag.
				Config: providerConfig(server.URL()) + `
resource "immuta_identifier" "test" {
  name        = "Primary colors"
  description = "Columns that hold a primary color"
  type        = "regex"
  regex       = "(red|green|blue)"
  tags        = ["Discovered.Does not exist"]
}
`,
				ExpectError: regexp.MustCompile(`immuta_tag`),
			},
		},
	})
}

func TestIdentifierResourceDetectsRemoteDrift(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)
	server.AddDomain("domain-1", "Finance")

	config := identifierConfig(server.URL(), "Primary colors", "Columns that hold a primary color", `"(red|green|blue)"`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllIdentifiersDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					for _, id := range identifierIDs(server) {
						server.SetIdentifierDescription(id, "Changed in the Immuta UI")
					}
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr(identifierResourceName, "description", "Columns that hold a primary color"),
			},
		},
	})
}

func TestDomainIdentifierResourceClonesRatherThanMoves(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)
	server.AddDomain("domain-1", "Finance")

	sourceID := server.SeedIdentifier("Credit card", "regex", `\d{16}`, []string{"Discovered.Credit card"})

	config := providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_domain_identifier" "test" {
  domain_id            = "domain-1"
  source_identifier_id = %q
}
`, sourceID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkSourceIdentifierSurvives(server, sourceID),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(domainIdentifierResourceName, "source_identifier_id", sourceID),
					resource.TestCheckResourceAttr(domainIdentifierResourceName, "domain_id", "domain-1"),
					resource.TestCheckResourceAttr(domainIdentifierResourceName, "name", "Credit card"),
					resource.TestCheckResourceAttr(domainIdentifierResourceName, "type", "regex"),
					resource.TestCheckTypeSetElemAttr(domainIdentifierResourceName, "tags.*", "Discovered.Credit card"),
					// The copy is a separate identifier, so there are now two.
					checkIdentifierCount(server, 2),
					checkCopyIsNotTheSource(sourceID),
				),
			},
			{
				// Applying the same configuration twice must not make a second
				// copy.
				Config:   config,
				PlanOnly: true,
			},
			{
				ResourceName:      domainIdentifierResourceName,
				ImportState:       true,
				ImportStateIdFunc: importIDForDomainIdentifier(sourceID),
				ImportStateVerify: true,
			},
		},
	})
}

// identifierConfig builds a tag and a regex identifier inside a domain.
func identifierConfig(serverURL, name, description, regex string) string {
	return providerConfig(serverURL) + fmt.Sprintf(`
resource "immuta_tag" "test" {
  name = "Discovered.Primary color"
}

resource "immuta_identifier" "test" {
  name        = %q
  description = %q
  domain_id   = "domain-1"
  type        = "regex"
  regex       = %s
  tags        = [immuta_tag.test.name]
}
`, name, description, regex)
}

func identifierIDs(server *testutil.SDDServer) []string {
	ids := []string{}
	for i := int64(1); i <= 20; i++ {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		if _, ok := server.Identifier(id); ok {
			ids = append(ids, id)
		}
	}

	return ids
}

func checkTagNames(server *testutil.SDDServer, want []string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.TagNames(); !reflect.DeepEqual(got, want) {
			return fmt.Errorf("the API holds tags %v, want %v", got, want)
		}

		return nil
	}
}

func checkIdentifierCount(server *testutil.SDDServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.IdentifierCount(); got != want {
			return fmt.Errorf("the API holds %d identifiers, want %d", got, want)
		}

		return nil
	}
}

func checkAllIdentifiersDestroyed(server *testutil.SDDServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.IdentifierCount(); got != 0 {
			return fmt.Errorf("%d identifiers are left in the API after destroy", got)
		}

		return nil
	}
}

// checkSourceIdentifierSurvives asserts that destroying the copy left the
// identifier it was copied from in place.
func checkSourceIdentifierSurvives(server *testutil.SDDServer, sourceID string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if _, ok := server.Identifier(sourceID); !ok {
			return errors.New("destroying the copy also deleted the identifier it was copied from")
		}

		if got := server.IdentifierCount(); got != 1 {
			return fmt.Errorf("the API holds %d identifiers after destroy, want only the source", got)
		}

		return nil
	}
}

func checkCopyIsNotTheSource(sourceID string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[domainIdentifierResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", domainIdentifierResourceName)
		}

		if resourceState.Primary.ID == sourceID {
			return errors.New("the copy reuses the ID of the identifier it was copied from, so Immuta moved it rather than copying")
		}

		return nil
	}
}

func importIDForDomainIdentifier(sourceID string) resource.ImportStateIdFunc {
	return func(state *terraform.State) (string, error) {
		resourceState, ok := state.RootModule().Resources[domainIdentifierResourceName]
		if !ok {
			return "", fmt.Errorf("%s is not in state", domainIdentifierResourceName)
		}

		return sourceID + "/" + resourceState.Primary.ID, nil
	}
}

// checkRootTagRequestsNeverDeleteTheHierarchy asserts how many creates were
// sent with rootTag, and that every one of them explicitly kept the existing
// children of the root.
func checkRootTagRequestsNeverDeleteTheHierarchy(server *testutil.SDDServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		requests := server.RootTagRequests()
		if len(requests) != want {
			return fmt.Errorf("%d tag creates used rootTag, want %d", len(requests), want)
		}

		for _, request := range requests {
			if !request.DeleteHierarchySent || request.DeleteHierarchy {
				return fmt.Errorf("a create under %q did not send deleteHierarchy=false explicitly", request.Name)
			}
		}

		return nil
	}
}

func domainIdentifierConfig(serverURL, sourceID string) string {
	return providerConfig(serverURL) + fmt.Sprintf(`
resource "immuta_domain_identifier" "test" {
  domain_id            = "domain-1"
  source_identifier_id = %q
}
`, sourceID)
}

// TestDomainIdentifierResourceReadsTheBulkCopyResponse runs the copy against
// the {count, createdIdentifiers, errors} envelope a real tenant returns, with
// each created entry as a bare id and as an object, since only the envelope
// has been observed.
func TestDomainIdentifierResourceReadsTheBulkCopyResponse(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	for name, asObjects := range map[string]bool{"created entries as ids": false, "created entries as objects": true} {
		t.Run(name, func(t *testing.T) {
			server := testutil.NewSDDServer()
			t.Cleanup(server.Close)
			server.AddDomain("domain-1", "Finance")
			server.SetBulkCopyCreatedAsObjects(asObjects)

			sourceID := server.SeedIdentifier("Credit card", "regex", `\d{16}`, []string{"Discovered.Credit card"})
			config := domainIdentifierConfig(server.URL(), sourceID)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				CheckDestroy:             checkSourceIdentifierSurvives(server, sourceID),
				Steps: []resource.TestStep{
					{
						Config: config,
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(domainIdentifierResourceName, "name", "Credit card"),
							resource.TestCheckResourceAttr(domainIdentifierResourceName, "domain_id", "domain-1"),
							resource.TestCheckTypeSetElemAttr(domainIdentifierResourceName, "tags.*", "Discovered.Credit card"),
							checkIdentifierCount(server, 2),
							checkCopyIsNotTheSource(sourceID),
						),
					},
					{Config: config, PlanOnly: true},
				},
			})
		})
	}
}

// TestDomainIdentifierResourceSurfacesCopyErrors reproduces the state left by
// an earlier failed apply: an orphaned copy with the same name is already in
// the domain. Immuta reports that in the errors array; the provider must fail
// straight away with that message rather than wait for a copy that was never
// made.
func TestDomainIdentifierResourceSurfacesCopyErrors(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewSDDServer()
	t.Cleanup(server.Close)
	server.AddDomain("domain-1", "Finance")

	sourceID := server.SeedIdentifier("Credit card", "regex", `\d{16}`, []string{"Discovered.Credit card"})
	server.SeedCopy(sourceID, "domain-1")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      domainIdentifierConfig(server.URL(), sourceID),
				ExpectError: regexp.MustCompile(`(?s)Immuta reported 1 error.*already exists in this\s+domain`),
			},
		},
	})

	// Nothing new was made: the source and the orphan only.
	if got := server.IdentifierCount(); got != 2 {
		t.Errorf("the API holds %d identifiers, want 2", got)
	}
}
