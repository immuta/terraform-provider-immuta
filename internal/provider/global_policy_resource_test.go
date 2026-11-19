package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/immuta/terraform-provider-immuta/internal/provider/testutil"
)

const globalPolicyResourceName = "immuta_global_policy.test"

// examplePoliciesDir holds the documented example policies shipped with the
// provider examples.
func examplePoliciesDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("..", "..", "examples", "resources", "immuta_global_policy", "policies"))
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestGlobalPolicyResourceFromExampleYAMLFiles(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewGlobalPolicyServer()
	t.Cleanup(server.Close)

	dir := examplePoliciesDir(t)
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) != 5 {
		t.Fatalf("expected the 5 documented example policies in %s, got %v (%v)", dir, files, err)
	}

	// The example uses for_each, which the plugin testing state checks cannot
	// address, so the same files are loaded with count here. Sorted, they are:
	// data-mask-constant, data-mask-hashing, subscription-entitlements-advanced,
	// subscription-entitlements and subscription-manual.
	config := providerConfig(server.URL()) + fmt.Sprintf(`
locals {
  files = sort(tolist(fileset(%[1]q, "*.yaml")))
}

resource "immuta_global_policy" "policies" {
  count  = length(local.files)
  policy = file("%[1]s/${local.files[count.index]}")
}
`, dir)
	hashing := "immuta_global_policy.policies.1"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGlobalPoliciesDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(hashing, "id"),
					resource.TestCheckResourceAttr(hashing, "policy_key", "data mask hashing"),
					resource.TestCheckResourceAttr(hashing, "name", "Hashing"),
					resource.TestCheckResourceAttr(hashing, "type", "data"),
					resource.TestCheckResourceAttr("immuta_global_policy.policies.3", "policy_key", "subscription entitlements"),
					resource.TestCheckResourceAttr("immuta_global_policy.policies.3", "type", "subscription"),
					checkEveryExamplePolicySaved(server, files),
				),
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}

// checkEveryExamplePolicySaved asserts that Immuta received each YAML file
// exactly as written, converted to JSON.
func checkEveryExamplePolicySaved(server *testutil.GlobalPolicyServer, files []string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Count(); got != len(files) {
			return fmt.Errorf("stored policies = %d, want %d", got, len(files))
		}

		for _, file := range files {
			content, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			want, err := parsePolicyDocument(string(content))
			if err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}

			_, got, ok := server.Policy(want["policyKey"].(string))
			if !ok {
				return fmt.Errorf("%s was not saved", filepath.Base(file))
			}
			if !reflect.DeepEqual(normalizeJSONNumbers(want), got) {
				return fmt.Errorf("%s: saved %v, want %v", filepath.Base(file), got, want)
			}
		}

		return nil
	}
}

const hashingPolicyYAML = `name: Hashing
policyKey: data mask hashing
type: data
actions:
  - rules:
      - type: Masking
        config:
          fields:
            - type: noTags
          maskingConfig:
            type: Hash
circumstances:
  - type: noTags
`

func TestGlobalPolicyResourceLifecycle(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewGlobalPolicyServer()
	t.Cleanup(server.Close)

	policyPath := filepath.Join(t.TempDir(), "hashing.yaml")
	write := func(content string) func() {
		return func() {
			if err := os.WriteFile(policyPath, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(hashingPolicyYAML)()

	config := providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_global_policy" "test" {
  policy = file(%q)
}
`, policyPath)

	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGlobalPoliciesDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(globalPolicyResourceName, "policy_key", "data mask hashing"),
					captureGlobalPolicyID(&firstID),
				),
			},
			{
				ResourceName:      globalPolicyResourceName,
				ImportState:       true,
				ImportStateVerify: true,
				// The V1 read cannot return the document in the V2 format.
				ImportStateVerifyIgnore: []string{"policy"},
			},
			{
				// Comments, key order and flow style are not changes.
				PreConfig: write(`# Hash every column without tags.
type: data
policyKey: data mask hashing
name: Hashing
circumstances: [{ type: noTags }]
actions:
  - rules:
      - config: { maskingConfig: { type: Hash }, fields: [{ type: noTags }] }
        type: Masking
`),
				Config:   config,
				PlanOnly: true,
			},
			{
				// Nor is the same policy written as JSON.
				PreConfig: write(`{"name": "Hashing", "policyKey": "data mask hashing", "type": "data",
  "actions": [{"rules": [{"type": "Masking", "config": {"fields": [{"type": "noTags"}], "maskingConfig": {"type": "Hash"}}}]}],
  "circumstances": [{"type": "noTags"}]}`),
				Config:   config,
				PlanOnly: true,
			},
			{
				// A real change updates the policy in place.
				PreConfig: write(strings.Replace(hashingPolicyYAML, "type: Hash", "type: Constant\n            constant: REDACTED", 1)),
				Config:    config,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkGlobalPolicyID(&firstID, true),
					checkStoredMaskingType(server, "data mask hashing", "Constant"),
					checkGlobalPolicyDeletes(server, 0),
				),
			},
			{
				// So does a rename, which keeps the policyKey.
				PreConfig: write(strings.Replace(hashingPolicyYAML, "name: Hashing", "name: Hash everything", 1)),
				Config:    config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(globalPolicyResourceName, "name", "Hash everything"),
					checkGlobalPolicyID(&firstID, true),
					checkGlobalPolicyDeletes(server, 0),
				),
			},
			{
				// A new policyKey replaces the policy rather than leaving the old
				// one behind.
				PreConfig: write(strings.Replace(hashingPolicyYAML, "policyKey: data mask hashing", "policyKey: data mask hashing v2", 1)),
				Config:    config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(globalPolicyResourceName, "policy_key", "data mask hashing v2"),
					checkGlobalPolicyID(&firstID, false),
					checkGlobalPolicyDeletes(server, 1),
					func(*terraform.State) error {
						if _, _, ok := server.Policy("data mask hashing"); ok || server.Count() != 1 {
							return errors.New("the policy with the old key should have been deleted")
						}

						return nil
					},
				),
			},
		},
	})
}

func TestGlobalPolicyResourceDetectsRenameAndDeletionInImmuta(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewGlobalPolicyServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_global_policy" "test" {
  policy = %q
}
`, hashingPolicyYAML)

	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGlobalPoliciesDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config, Check: captureGlobalPolicyID(&id)},
			{
				PreConfig:          func() { server.Rename(id, "Renamed in the Immuta UI") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying saves the document again, restoring the name.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(globalPolicyResourceName, "name", "Hashing"),
					checkGlobalPolicyID(&id, true),
				),
			},
			{
				PreConfig:          func() { server.Remove(id) },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, ok := state.RootModule().Resources[globalPolicyResourceName]; ok {
						return errors.New("the deleted policy should have been removed from state")
					}

					return nil
				},
			},
			{
				Config: config,
				Check: func(*terraform.State) error {
					if server.Count() != 1 {
						return errors.New("the policy should have been recreated")
					}

					return nil
				},
			},
		},
	})
}

func TestGlobalPolicyResourceDetectsContentChangesInImmuta(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewGlobalPolicyServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_global_policy" "test" {
  policy = %q
}
`, hashingPolicyYAML)

	var id string
	var savesBefore int

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGlobalPoliciesDestroyed(server),
		Steps: []resource.TestStep{
			{Config: config, Check: captureGlobalPolicyID(&id)},
			{
				// An unchanged policy plans nothing.
				Config:   config,
				PlanOnly: true,
			},
			{
				// An edit to the content in Immuta, keeping the name and key.
				PreConfig: func() {
					savesBefore = server.Saves()
					server.EditActions(id, []any{map[string]any{"type": "edited in the Immuta UI"}})
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying saves the configured document again, in place. The
				// harness then checks that the next plan is empty, so the new
				// content was recorded.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkGlobalPolicyID(&id, true),
					checkGlobalPolicyDeletes(server, 0),
					checkStoredMaskingType(server, "data mask hashing", "Hash"),
					func(*terraform.State) error {
						if server.Saves() != savesBefore+1 {
							return fmt.Errorf("saves = %d, want the document saved again once", server.Saves()-savesBefore)
						}

						return nil
					},
				),
			},
		},
	})
}

// A policy with no recorded content, as in state written before the content
// was recorded, may already have been changed in Immuta. Its document is
// saved again once rather than the current content being trusted.
func TestGlobalPolicyResourceSavesAgainWithoutRecordedContent(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewGlobalPolicyServer()
	t.Cleanup(server.Close)

	config := providerConfig(server.URL()) + fmt.Sprintf(`
resource "immuta_global_policy" "test" {
  policy = %q
}
`, hashingPolicyYAML)

	var id string
	var savesBefore int

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGlobalPoliciesDestroyed(server),
		Steps: []resource.TestStep{
			{
				// The read that resolves the id succeeds and the read back
				// after the save fails, so nothing is recorded. The content is
				// then changed in Immuta.
				PreConfig: func() { server.FailGetAfter(1) },
				Config:    config,
				Check: resource.ComposeAggregateTestCheckFunc(
					captureGlobalPolicyID(&id),
					func(*terraform.State) error {
						savesBefore = server.Saves()
						server.EditActions(id, []any{map[string]any{"type": "edited in the Immuta UI"}})

						return nil
					},
				),
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying saves the document again and records its content,
				// and the harness checks that the next plan is empty.
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					checkGlobalPolicyID(&id, true),
					checkGlobalPolicyDeletes(server, 0),
					checkStoredMaskingType(server, "data mask hashing", "Hash"),
					func(*terraform.State) error {
						if server.Saves() != savesBefore+1 {
							return fmt.Errorf("saves = %d, want the document saved again once", server.Saves()-savesBefore)
						}

						return nil
					},
				),
			},
		},
	})
}

// memoryPrivateState is a privateState kept in memory. Like the framework's
// private state, setting an empty value removes the key.
type memoryPrivateState map[string][]byte

func (m memoryPrivateState) GetKey(_ context.Context, key string) ([]byte, diag.Diagnostics) {
	return m[key], nil
}

func (m memoryPrivateState) SetKey(_ context.Context, key string, value []byte) diag.Diagnostics {
	if len(value) == 0 {
		delete(m, key)

		return nil
	}
	m[key] = value

	return nil
}

func TestGlobalPolicyFingerprintPrivateState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	private := memoryPrivateState{}

	if got, diags := getGlobalPolicyFingerprint(ctx, private); got != "" || diags.HasError() {
		t.Errorf("nothing recorded: got %q, %v, want an empty fingerprint", got, diags)
	}

	if diags := setGlobalPolicyFingerprint(ctx, private, "abc123"); diags.HasError() {
		t.Fatalf("setGlobalPolicyFingerprint: %v", diags)
	}
	if !json.Valid(private[globalPolicyFingerprintKey]) {
		t.Errorf("stored value %s is not JSON, which private state requires", private[globalPolicyFingerprintKey])
	}
	if got, _ := getGlobalPolicyFingerprint(ctx, private); got != "abc123" {
		t.Errorf("round trip = %q, want abc123", got)
	}

	if diags := setGlobalPolicyFingerprint(ctx, private, ""); diags.HasError() {
		t.Fatalf("clearing the fingerprint: %v", diags)
	}
	if _, ok := private[globalPolicyFingerprintKey]; ok {
		t.Error("an empty fingerprint should clear the key")
	}

	private[globalPolicyFingerprintKey] = []byte(`{"not": "a string"}`)
	if got, diags := getGlobalPolicyFingerprint(ctx, private); got != "" || diags.HasError() {
		t.Errorf("unreadable value: got %q, %v, want it treated as nothing recorded", got, diags)
	}
}

func TestGlobalPolicyResourceUsesAnIDInTheSaveResponse(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewGlobalPolicyServer()
	server.RespondWithID()
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories(),
		CheckDestroy:             checkAllGlobalPoliciesDestroyed(server),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL()) + fmt.Sprintf("resource \"immuta_global_policy\" \"test\" {\n  policy = %q\n}\n", hashingPolicyYAML),
				Check: func(*terraform.State) error {
					for _, request := range server.Requests() {
						if request.Path == "/policy/global" {
							return errors.New("policies were searched although the save response held the id")
						}
					}

					return nil
				},
			},
		},
	})
}

func TestGlobalPolicyResourceValidatesTheDocumentAtPlanTime(t *testing.T) {
	skipWithoutTerraformCLI(t)
	clearProviderEnvironment(t)

	server := testutil.NewGlobalPolicyServer()
	t.Cleanup(server.Close)

	cases := map[string]struct {
		document string
		want     string
	}{
		"not a mapping":     {"- name: Hashing\n", `must\s+be\s+a\s+mapping`},
		"missing policyKey": {strings.Replace(hashingPolicyYAML, "policyKey: data mask hashing\n", "", 1), `needs\s+a\s+policyKey`},
		"unknown type":      {strings.Replace(hashingPolicyYAML, "type: data", "type: masking", 1), `policy\s+type\s+must\s+be`},
		"missing actions":   {"name: N\npolicyKey: k\ntype: data\n", `needs\s+actions`},
		"invalid yaml":      {"name: [unclosed\n", `not\s+valid\s+YAML`},
		"two documents":     {hashingPolicyYAML + "---\n" + hashingPolicyYAML, `single\s+YAML\s+document`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config:      providerConfig(server.URL()) + fmt.Sprintf("resource \"immuta_global_policy\" \"test\" {\n  policy = %q\n}\n", tc.document),
						PlanOnly:    true,
						ExpectError: regexp.MustCompile(tc.want),
					},
				},
			})
		})
	}

	if got := len(server.Requests()); got != 0 {
		t.Errorf("the API received %d requests, want none for an invalid document", got)
	}
}

func TestCheckPolicyDocumentWarnsAboutUndocumentedKeys(t *testing.T) {
	t.Parallel()

	document, err := parsePolicyDocument(hashingPolicyYAML + "staged: false\ncircumstanceOprator: any\n")
	if err != nil {
		t.Fatal(err)
	}

	fields, warnings, err := checkPolicyDocument(document)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if fields != (policyDocumentFields{PolicyKey: "data mask hashing", Name: "Hashing", Type: "data"}) {
		t.Errorf("fields = %+v", fields)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"circumstanceOprator"`) {
		t.Errorf("warnings = %v, want one about the misspelt circumstanceOprator", warnings)
	}
}

func TestPolicyDocumentSemanticEquality(t *testing.T) {
	t.Parallel()

	value := func(text string) yamlDocumentValue {
		return yamlDocumentValue{StringValue: types.StringValue(text)}
	}

	cases := map[string]struct {
		a, b string
		want bool
	}{
		"identical":          {hashingPolicyYAML, hashingPolicyYAML, true},
		"comments and order": {"a: 1\nb: [x, y]\n", "# note\nb:\n  - x\n  - y\na: 1\n", true},
		"yaml and json":      {"a: 1\nb: [x, y]\n", `{"b": ["x", "y"], "a": 1}`, true},
		"list order matters": {"b: [x, y]\n", "b: [y, x]\n", false},
		"value changed":      {"a: 1\n", "a: 2\n", false},
		"string vs number":   {"a: 1\n", "a: \"1\"\n", false},
		"unparseable":        {"a: [\n", "a: [\n ", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, diags := value(tc.a).StringSemanticEquals(t.Context(), value(tc.b))
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.want {
				t.Errorf("semantically equal = %t, want %t", got, tc.want)
			}
		})
	}
}

func captureGlobalPolicyID(id *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		resourceState, ok := state.RootModule().Resources[globalPolicyResourceName]
		if !ok {
			return fmt.Errorf("%s is not in state", globalPolicyResourceName)
		}
		*id = resourceState.Primary.ID

		return nil
	}
}

// checkGlobalPolicyID asserts that the policy kept, or did not keep, its id.
func checkGlobalPolicyID(previous *string, same bool) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		id := state.RootModule().Resources[globalPolicyResourceName].Primary.ID
		if same && id != *previous {
			return fmt.Errorf("id = %q, want %q: the policy was replaced rather than updated", id, *previous)
		}
		if !same && id == *previous {
			return fmt.Errorf("id = %q, want a new id: the policy should have been replaced", id)
		}

		return nil
	}
}

func checkGlobalPolicyDeletes(server *testutil.GlobalPolicyServer, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Deletes(); got != want {
			return fmt.Errorf("deletes = %d, want %d", got, want)
		}

		return nil
	}
}

func checkStoredMaskingType(server *testutil.GlobalPolicyServer, policyKey, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		_, document, ok := server.Policy(policyKey)
		if !ok {
			return fmt.Errorf("policy %q is not stored", policyKey)
		}

		rule := document["actions"].([]any)[0].(map[string]any)["rules"].([]any)[0].(map[string]any)
		got := rule["config"].(map[string]any)["maskingConfig"].(map[string]any)["type"]
		if got != want {
			return fmt.Errorf("stored masking type = %v, want %s", got, want)
		}

		return nil
	}
}

func checkAllGlobalPoliciesDestroyed(server *testutil.GlobalPolicyServer) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := server.Count(); got != 0 {
			return fmt.Errorf("%d global policies still exist after destroy", got)
		}

		return nil
	}
}

// normalizeJSONNumbers round-trips a document through JSON, so that numbers
// compare equal to what a JSON API decodes.
func normalizeJSONNumbers(document map[string]any) map[string]any {
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		panic(err)
	}

	return decoded
}
