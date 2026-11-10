package provider

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const frameworkResourceName = "immuta_framework.test"

// frameworkConfig is the documented HR example, plus a second tag with a
// sensitivity and a second rule using the other two criteria.
func frameworkConfig(host, description string, active bool) string {
	return providerConfig(host) + fmt.Sprintf(`
resource "immuta_framework" "test" {
  short_name  = "HR Information"
  name        = "HR Information Framework"
  description = %q
  active      = %t

  tags = [
    {
      name   = "HR Framework.Internal Employee Data"
      source = "curated"
    },
    {
      name   = "HR Framework.Salary"
      source = "curated"
      sensitivities = [
        { dimension = "confidentiality", sensitivity = 2 },
      ]
    },
  ]

  rules = [
    {
      name               = "HR Rule 1"
      classification_tag = { name = "HR Framework.Internal Employee Data", source = "curated" }
      neighbor_column_tags = [
        { name = "Employee Name", source = "curated" },
      ]
    },
    {
      name               = "HR Rule 2"
      classification_tag = { name = "HR Framework.Salary", source = "curated" }
      column_tags = [
        { name = "Discovered.Salary", source = "curated" },
        { name = "Discovered.Compensation", source = "curated" },
      ]
      table_tags = [
        { name = "Payroll", source = "curated" },
      ]
    },
  ]
}
`, description, active)
}

func TestFrameworkResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewFrameworkServer()
	t.Cleanup(server.Close)

	var firstID, firstVersion string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllFrameworksDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: frameworkConfig(server.URL(), "Finds internal employee information.", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(frameworkResourceName, "id"),
					resource.TestCheckResourceAttrSet(frameworkResourceName, "version"),
					resource.TestCheckResourceAttr(frameworkResourceName, "short_name", "HR Information"),
					resource.TestCheckResourceAttr(frameworkResourceName, "active", "true"),
					resource.TestCheckResourceAttr(frameworkResourceName, "created_by", "2"),
					resource.TestCheckResourceAttrSet(frameworkResourceName, "created_at"),
					resource.TestCheckResourceAttr(frameworkResourceName, "tags.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs(frameworkResourceName, "tags.*", map[string]string{
						"name":                        "HR Framework.Salary",
						"sensitivities.#":             "1",
						"sensitivities.0.dimension":   "confidentiality",
						"sensitivities.0.sensitivity": "2",
					}),
					resource.TestCheckResourceAttr(frameworkResourceName, "rules.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs(frameworkResourceName, "rules.*", map[string]string{
						"name":                      "HR Rule 1",
						"classification_tag.name":   "HR Framework.Internal Employee Data",
						"classification_tag.source": "curated",
						"neighbor_column_tags.#":    "1",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(frameworkResourceName, "rules.*", map[string]string{
						"name":          "HR Rule 2",
						"column_tags.#": "2",
						"table_tags.#":  "1",
					}),
					checkFrameworkMatchesServer(server),
					captureFrameworkIdentity(&firstID, &firstVersion),
				),
			},
			{
				// Immuta returns tags and rules in a different order, which must
				// not produce a diff.
				Config:   frameworkConfig(server.URL(), "Finds internal employee information.", true),
				PlanOnly: true,
			},
			{
				ResourceName:      frameworkResourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Updates are applied in place, and false is sent rather than
				// omitted.
				Config: frameworkConfig(server.URL(), "Updated description.", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(frameworkResourceName, "description", "Updated description."),
					resource.TestCheckResourceAttr(frameworkResourceName, "active", "false"),
					checkFrameworkUpdatedInPlace(server, &firstID, &firstVersion),
					checkLastFrameworkBody(server, "active", false),
					checkFrameworkMatchesServer(server),
				),
			},
			{
				Config:   frameworkConfig(server.URL(), "Updated description.", false),
				PlanOnly: true,
			},
		},
	})
}

// TestFrameworkResourceRemovesOptionalLists drops a sensitivity and a set of
// criteria, and checks that Immuta receives empty lists rather than keeping
// the old values.
func TestFrameworkResourceRemovesOptionalLists(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewFrameworkServer()
	t.Cleanup(server.Close)

	minimal := providerConfig(server.URL()) + `
resource "immuta_framework" "test" {
  short_name  = "HR Information"
  name        = "HR Information Framework"
  description = "Finds internal employee information."
  active      = true

  tags = [
    { name = "HR Framework.Salary", source = "curated" },
  ]

  rules = [
    {
      name               = "HR Rule 2"
      classification_tag = { name = "HR Framework.Salary", source = "curated" }
      column_tags = [
        { name = "Discovered.Salary", source = "curated" },
      ]
    },
  ]
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllFrameworksDestroyed(server),
		Steps: []resource.TestStep{
			{Config: frameworkConfig(server.URL(), "Finds internal employee information.", true)},
			{
				Config: minimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(frameworkResourceName, "tags.#", "1"),
					resource.TestCheckResourceAttr(frameworkResourceName, "tags.0.name", "HR Framework.Salary"),
					resource.TestCheckNoResourceAttr(frameworkResourceName, "tags.0.sensitivities"),
					resource.TestCheckResourceAttr(frameworkResourceName, "rules.#", "1"),
					resource.TestCheckNoResourceAttr(frameworkResourceName, "rules.0.table_tags"),
					resource.TestCheckNoResourceAttr(frameworkResourceName, "rules.0.neighbor_column_tags"),
					checkFrameworkMatchesServer(server),
					checkLastFrameworkBody(server, "rules", []any{map[string]any{
						"name":               "HR Rule 2",
						"classificationTag":  map[string]any{"name": "HR Framework.Salary", "source": "curated"},
						"columnTags":         []any{map[string]any{"name": "Discovered.Salary", "source": "curated"}},
						"neighborColumnTags": []any{},
						"tableTags":          []any{},
					}}),
					checkLastFrameworkBody(server, "tags", []any{map[string]any{
						"name":          "HR Framework.Salary",
						"source":        "curated",
						"sensitivities": []any{},
					}}),
				),
			},
			{Config: minimal, PlanOnly: true},
		},
	})
}

func TestFrameworkResourceDetectsDrift(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewFrameworkServer()
	t.Cleanup(server.Close)

	config := frameworkConfig(server.URL(), "Finds internal employee information.", true)
	var id, version string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  captureFrameworkIdentity(&id, &version),
			},
			{
				PreConfig:          func() { server.SetDescription(id, "Changed in the Immuta UI") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying puts the configured description back.
				Config: config,
				Check: func(*terraform.State) error {
					framework, ok := server.Framework(id)
					if !ok || framework.Description != "Finds internal employee information." {
						return fmt.Errorf("stored description = %q, want the configured description", framework.Description)
					}

					return nil
				},
			},
		},
	})
}

func TestFrameworkResourceHandlesRemoteDeletion(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewFrameworkServer()
	t.Cleanup(server.Close)

	config := frameworkConfig(server.URL(), "Finds internal employee information.", true)
	var id, version string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllFrameworksDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  captureFrameworkIdentity(&id, &version),
			},
			{
				PreConfig:          func() { server.Remove(id) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[frameworkResourceName]; ok {
						return errors.New("the deleted framework should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check: func(*terraform.State) error {
					if got := server.Count(); got != 1 {
						return fmt.Errorf("stored frameworks = %d, want the framework recreated", got)
					}

					return nil
				},
			},
		},
	})
}

func TestFrameworkResourceValidatesAtPlanTime(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewFrameworkServer()
	t.Cleanup(server.Close)

	valid := frameworkConfig(server.URL(), "Finds internal employee information.", true)

	var rules strings.Builder
	for i := range 51 {
		fmt.Fprintf(&rules, `
    {
      name               = "Rule %d"
      classification_tag = { name = "HR Framework.Salary", source = "curated" }
    },`, i)
	}
	tooManyRules := providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_framework" "test" {
  short_name  = "Too many"
  name        = "Too many rules"
  description = "Has 51 rules"
  active      = true
  tags        = [{ name = "HR Framework.Salary", source = "curated" }]
  rules       = [%s
  ]
}
`, rules.String())

	cases := map[string]struct {
		config string
		want   *regexp.Regexp
	}{
		"sensitivity out of range": {
			strings.Replace(valid, "sensitivity = 2", "sensitivity = 3", 1),
			regexp.MustCompile(`value must\s+be\s+between\s+0\s+and\s+2`),
		},
		"undocumented dimension": {
			strings.Replace(valid, `dimension = "confidentiality"`, `dimension = "integrity"`, 1),
			regexp.MustCompile(`value must\s+be\s+one\s+of`),
		},
		"empty criteria": {
			strings.Replace(valid, "table_tags = [\n        { name = \"Payroll\", source = \"curated\" },\n      ]", "table_tags = []", 1),
			regexp.MustCompile(`set must\s+contain\s+at\s+least\s+1\s+elements`),
		},
		"too many rules": {
			tooManyRules,
			regexp.MustCompile(`set must\s+contain\s+at\s+most\s+50\s+elements`),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.config == valid {
				t.Fatal("the test case did not change the configuration")
			}

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      tc.config,
						PlanOnly:    true,
						ExpectError: tc.want,
					},
				},
			})
		})
	}

	if got := len(server.Requests()); got != 0 {
		t.Errorf("the API received %d requests, want none for an invalid configuration", got)
	}
}

func captureFrameworkIdentity(id, version *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[frameworkResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", frameworkResourceName)
		}

		*id = resourceState.Primary.ID
		*version = resourceState.Primary.Attributes["version"]

		return nil
	}
}

// checkFrameworkUpdatedInPlace asserts that the framework kept its id, that
// Immuta assigned a new version, and that it was not recreated.
func checkFrameworkUpdatedInPlace(server *testutil.FrameworkServer, firstID, firstVersion *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		attributes := state.RootModule().Resources[frameworkResourceName].Primary.Attributes

		if attributes["id"] != *firstID {
			return fmt.Errorf("id = %q, want %q: the framework was replaced rather than updated", attributes["id"], *firstID)
		}
		if attributes["version"] == *firstVersion {
			return errors.New("version did not change after an update")
		}
		if got := server.Count(); got != 1 {
			return fmt.Errorf("stored frameworks = %d, want 1", got)
		}

		return nil
	}
}

func checkLastFrameworkBody(server *testutil.FrameworkServer, key string, want any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		body := server.LastBody()

		got, ok := body[key]
		if !ok {
			return fmt.Errorf("the last request body did not contain %q", key)
		}
		if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("last request body %s = %v, want %v", key, got, want)
		}

		return nil
	}
}

func checkAllFrameworksDestroyed(server *testutil.FrameworkServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Count(); got != 0 {
			return fmt.Errorf("%d frameworks still exist after destroy", got)
		}

		return nil
	}
}

// checkFrameworkMatchesServer compares every attribute in state, including
// each tag and rule, with what the fake API stores.
func checkFrameworkMatchesServer(server *testutil.FrameworkServer) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[frameworkResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", frameworkResourceName)
		}

		framework, ok := server.Framework(resourceState.Primary.ID)
		if !ok {
			return fmt.Errorf("framework %s is not stored by the fake API", resourceState.Primary.ID)
		}

		return testutil.FrameworkParityError(&framework, resourceState.Primary.Attributes)
	}
}
