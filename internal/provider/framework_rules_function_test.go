package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

func curated(name string) immutaapi.FrameworkTagRef {
	return immutaapi.FrameworkTagRef{Name: name, Source: "curated"}
}

func TestDecodeFrameworkRulesYAML(t *testing.T) {
	t.Parallel()

	wantHR := []immutaapi.FrameworkRule{
		{
			Name:               "HR Rule 1",
			ClassificationTag:  curated("HR Framework.Internal"),
			NeighborColumnTags: []immutaapi.FrameworkTagRef{curated("Employee Name")},
		},
		{
			Name:              "HR Rule 2",
			ClassificationTag: curated("HR Framework.Salary"),
			ColumnTags:        []immutaapi.FrameworkTagRef{curated("Discovered.Salary"), curated("Discovered.Pay")},
			TableTags:         []immutaapi.FrameworkTagRef{curated("Payroll")},
		},
	}

	cases := map[string]struct {
		document string
		want     []immutaapi.FrameworkRule
	}{
		"terraform key names": {
			document: `
- name: HR Rule 1
  classification_tag: { name: HR Framework.Internal, source: curated }
  neighbor_column_tags:
    - { name: Employee Name, source: curated }
- name: HR Rule 2
  classification_tag:
    name: HR Framework.Salary
    source: curated
  column_tags:
    - name: Discovered.Salary
      source: curated
    - name: Discovered.Pay
      source: curated
  table_tags:
    - { name: Payroll, source: curated }
`,
			want: wantHR,
		},
		"immuta api key names": {
			document: `
- name: HR Rule 1
  classificationTag: { name: HR Framework.Internal, source: curated }
  columnTags: []
  neighborColumnTags: [{ name: Employee Name, source: curated }]
  tableTags: []
- name: HR Rule 2
  classificationTag: { name: HR Framework.Salary, source: curated }
  columnTags:
    - { name: Discovered.Salary, source: curated }
    - { name: Discovered.Pay, source: curated }
  tableTags: [{ name: Payroll, source: curated }]
`,
			want: wantHR,
		},
		"json": {
			document: `[{"name": "HR Rule 1", "classificationTag": {"name": "HR Framework.Internal", "source": "curated"},
				"neighborColumnTags": [{"name": "Employee Name", "source": "curated"}]}]`,
			want: wantHR[:1],
		},
		"empty list": {
			document: `[]`,
			want:     []immutaapi.FrameworkRule{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeFrameworkRulesYAML(tc.document)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rules = %+v\nwant    %+v", got, tc.want)
			}
		})
	}
}

func TestDecodeFrameworkRulesYAMLRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		document string
		want     []string
	}{
		// The whole point of the function: yamldecode would drop these.
		"misspelt criteria key": {
			document: "- name: R\n  classification_tag: { name: C, source: curated }\n  colum_tags: [{ name: T, source: curated }]\n",
			want:     []string{`line 3: unknown key "colum_tags"`},
		},
		"unknown tag key": {
			document: "- name: R\n  classification_tag: { name: C, sorce: curated }\n",
			want:     []string{`line 2: unknown key "sorce"`},
		},
		"both spellings": {
			document: "- name: R\n  classification_tag: { name: C, source: curated }\n  column_tags: [{ name: A, source: curated }]\n  columnTags: [{ name: B, source: curated }]\n",
			want:     []string{`rule 1: "R"`, "column_tags is given twice"},
		},
		"missing name": {
			document: "- classification_tag: { name: C, source: curated }\n",
			want:     []string{"rule 1", "name is required"},
		},
		"missing classification tag": {
			document: "- name: R\n  table_tags: [{ name: T, source: curated }]\n",
			want:     []string{`"R"`, "classification_tag is required"},
		},
		"missing source": {
			document: "- name: OK\n  classification_tag: { name: C, source: curated }\n- name: R\n  classification_tag: { name: C, source: curated }\n  table_tags: [{ name: T }]\n",
			want:     []string{"rule 2", `the tag "T" in table_tags needs a source`},
		},
		"criteria tag without name": {
			document: "- name: R\n  classification_tag: { name: C, source: curated }\n  column_tags: [{ source: curated }]\n",
			want:     []string{"every tag in column_tags needs a name"},
		},
		"not a list": {
			document: "rules:\n  - name: R\n",
			want:     []string{"line 1: expected a list of rules, got a mapping"},
		},
		"empty document": {
			document: "  \n",
			want:     []string{"empty"},
		},
		"tag given as text": {
			document: "- name: R\n  classification_tag: HR Framework.Salary\n",
			want:     []string{"line 2: expected a tag with name and source, got text"},
		},
		"two documents": {
			document: "[]\n---\n[]\n",
			want:     []string{"single YAML document"},
		},
		"invalid yaml": {
			document: "- name: [unclosed\n",
			want:     []string{"not valid YAML"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeFrameworkRulesYAML(tc.document)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "provider.yaml") {
				t.Errorf("error %q leaks an internal Go type name", err)
			}
		})
	}
}

const frameworkRulesYAML = `# Loaded with provider::immuta::decode_framework_rules(file(...))
- name: HR Rule 1
  classification_tag: { name: HR Framework.Internal Employee Data, source: curated }
  neighbor_column_tags:
    - { name: Employee Name, source: curated }

- name: HR Rule 2
  classificationTag: { name: HR Framework.Salary, source: curated }
  columnTags:
    - { name: Discovered.Salary, source: curated }
  tableTags: []
`

func frameworkRulesFileConfig(host, rulesPath string) string {
	return providerConfig(host) + fmt.Sprintf(`
resource "immuta_framework" "test" {
  short_name  = "HR Information"
  name        = "HR Information Framework"
  description = "Rules loaded from YAML."
  active      = true

  tags = [
    { name = "HR Framework.Internal Employee Data", source = "curated" },
    { name = "HR Framework.Salary", source = "curated" },
  ]

  rules = provider::immuta::decode_framework_rules(file(%q))
}
`, rulesPath)
}

func writeRulesFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %s", path, err)
	}
}

func TestDecodeFrameworkRulesFunctionFromFile(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewFrameworkServer()
	t.Cleanup(server.Close)

	rulesPath := filepath.Join(t.TempDir(), "rules.yaml")
	writeRulesFile(t, rulesPath, frameworkRulesYAML)
	config := frameworkRulesFileConfig(server.URL(), rulesPath)

	var id, version string

	resource.UnitTest(t, resource.TestCase{
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_8_0)},
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllFrameworksDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(frameworkResourceName, "rules.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs(frameworkResourceName, "rules.*", map[string]string{
						"name":                    "HR Rule 2",
						"classification_tag.name": "HR Framework.Salary",
						"column_tags.#":           "1",
					}),
					checkFrameworkMatchesServer(server),
					captureFrameworkIdentity(&id, &version),
				),
			},
			{
				// Re-reading the unchanged file is a no-op.
				Config:   config,
				PlanOnly: true,
			},
			{
				// Editing the file updates the framework in place.
				PreConfig: func() {
					writeRulesFile(t, rulesPath, frameworkRulesYAML+`
- name: HR Rule 3
  classification_tag: { name: HR Framework.Salary, source: curated }
  table_tags: [{ name: Payroll, source: curated }]
`)
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(frameworkResourceName, "rules.#", "3"),
					checkFrameworkUpdatedInPlace(server, &id, &version),
					checkFrameworkMatchesServer(server),
				),
			},
			{
				// A misspelt key, which yamldecode would silently drop, fails the
				// plan and leaves the framework untouched.
				PreConfig: func() {
					writeRulesFile(t, rulesPath, strings.Replace(frameworkRulesYAML, "neighbor_column_tags", "neighbour_column_tags", 1))
				},
				Config:      config,
				ExpectError: regexp.MustCompile(`unknown key "neighbour_column_tags"`),
			},
			{
				PreConfig: func() { writeRulesFile(t, rulesPath, frameworkRulesYAML) },
				Config:    config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(frameworkResourceName, "rules.#", "2"),
					checkFrameworkMatchesServer(server),
				),
			},
		},
	})
}
