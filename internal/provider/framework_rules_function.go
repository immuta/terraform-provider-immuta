package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"gopkg.in/yaml.v3"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

var _ function.Function = (*decodeFrameworkRulesFunction)(nil)

// NewDecodeFrameworkRulesFunction returns the decode_framework_rules function.
//
// Terraform's own yamldecode silently drops keys that the rules schema does
// not know, so a misspelt criterion produces a rule without it. This function
// parses strictly instead, and rejects anything it does not recognise.
func NewDecodeFrameworkRulesFunction() function.Function {
	return &decodeFrameworkRulesFunction{}
}

type decodeFrameworkRulesFunction struct{}

func (f *decodeFrameworkRulesFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "decode_framework_rules"
}

func (f *decodeFrameworkRulesFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Decode framework rules from YAML",
		MarkdownDescription: "Strictly decodes a YAML (or JSON) list of framework rules into the value expected by " +
			"the `rules` attribute of `immuta_framework`. Unknown keys, missing names and missing sources are " +
			"errors, unlike `yamldecode`, which drops unknown keys silently. Keys may be written as in Terraform " +
			"(`classification_tag`, `column_tags`, `neighbor_column_tags`, `table_tags`) or as in the Immuta API " +
			"(`classificationTag`, `columnTags`, `neighborColumnTags`, `tableTags`).",
		Parameters: []function.Parameter{
			function.StringParameter{
				Name:                "document",
				MarkdownDescription: "YAML document containing a list of rules, typically read with `file()`.",
			},
		},
		Return: function.ListReturn{ElementType: frameworkRuleType},
	}
}

func (f *decodeFrameworkRulesFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var document string

	resp.Error = req.Arguments.Get(ctx, &document)
	if resp.Error != nil {
		return
	}

	rules, err := decodeFrameworkRulesYAML(document)
	if err != nil {
		resp.Error = function.NewArgumentFuncError(0, err.Error())

		return
	}

	values := make([]attr.Value, 0, len(rules))
	for _, rule := range rules {
		value, diags := frameworkRuleValue(rule)
		if diags.HasError() {
			resp.Error = function.FuncErrorFromDiags(ctx, diags)

			return
		}
		values = append(values, value)
	}

	list, diags := types.ListValue(frameworkRuleType, values)
	if diags.HasError() {
		resp.Error = function.FuncErrorFromDiags(ctx, diags)

		return
	}

	resp.Error = resp.Result.Set(ctx, list)
}

// yamlFrameworkTagRef is a tag reference in a rules document.
type yamlFrameworkTagRef struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
}

// yamlFrameworkRule is a rule in a rules document. Every key can be written
// as in Terraform or as in the Immuta API, but not both.
type yamlFrameworkRule struct {
	Name string `yaml:"name"`

	ClassificationTag    *yamlFrameworkTagRef `yaml:"classification_tag"`
	ClassificationTagAPI *yamlFrameworkTagRef `yaml:"classificationTag"`

	ColumnTags            []yamlFrameworkTagRef `yaml:"column_tags"`
	ColumnTagsAPI         []yamlFrameworkTagRef `yaml:"columnTags"`
	NeighborColumnTags    []yamlFrameworkTagRef `yaml:"neighbor_column_tags"`
	NeighborColumnTagsAPI []yamlFrameworkTagRef `yaml:"neighborColumnTags"`
	TableTags             []yamlFrameworkTagRef `yaml:"table_tags"`
	TableTagsAPI          []yamlFrameworkTagRef `yaml:"tableTags"`
}

// decodeFrameworkRulesYAML strictly decodes a single YAML document holding a
// list of rules. JSON is accepted too, as it is valid YAML.
func decodeFrameworkRulesYAML(document string) ([]immutaapi.FrameworkRule, error) {
	decoder := yaml.NewDecoder(strings.NewReader(document))
	decoder.KnownFields(true)

	var decoded []yamlFrameworkRule
	if err := decoder.Decode(&decoded); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the rules document is empty")
		}

		return nil, describeYAMLError(err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("the rules document must contain a single YAML document")
	}

	rules := make([]immutaapi.FrameworkRule, 0, len(decoded))
	for i, entry := range decoded {
		rule, err := entry.toRule()
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i+1, err)
		}
		rules = append(rules, rule)
	}

	return rules, nil
}

func (r yamlFrameworkRule) toRule() (immutaapi.FrameworkRule, error) {
	var rule immutaapi.FrameworkRule

	if strings.TrimSpace(r.Name) == "" {
		return rule, errors.New("name is required")
	}
	rule.Name = r.Name

	classificationTag, err := pickTagRef("classification_tag", r.ClassificationTag, r.ClassificationTagAPI)
	if err != nil {
		return rule, fmt.Errorf("%q: %w", r.Name, err)
	}
	if classificationTag == nil {
		return rule, fmt.Errorf("%q: classification_tag is required", r.Name)
	}
	if rule.ClassificationTag, err = classificationTag.toRef("classification_tag"); err != nil {
		return rule, fmt.Errorf("%q: %w", r.Name, err)
	}

	criteria := []struct {
		key       string
		terraform []yamlFrameworkTagRef
		api       []yamlFrameworkTagRef
		target    *[]immutaapi.FrameworkTagRef
	}{
		{"column_tags", r.ColumnTags, r.ColumnTagsAPI, &rule.ColumnTags},
		{"neighbor_column_tags", r.NeighborColumnTags, r.NeighborColumnTagsAPI, &rule.NeighborColumnTags},
		{"table_tags", r.TableTags, r.TableTagsAPI, &rule.TableTags},
	}

	for _, c := range criteria {
		refs, err := pickTagRefs(c.key, c.terraform, c.api)
		if err != nil {
			return rule, fmt.Errorf("%q: %w", r.Name, err)
		}

		// An empty list means no criteria, which the resource stores as null.
		for _, ref := range refs {
			converted, err := ref.toRef(c.key)
			if err != nil {
				return rule, fmt.Errorf("%q: %w", r.Name, err)
			}
			*c.target = append(*c.target, converted)
		}
	}

	return rule, nil
}

func (t yamlFrameworkTagRef) toRef(key string) (immutaapi.FrameworkTagRef, error) {
	if strings.TrimSpace(t.Name) == "" {
		return immutaapi.FrameworkTagRef{}, fmt.Errorf("every tag in %s needs a name", key)
	}
	if strings.TrimSpace(t.Source) == "" {
		return immutaapi.FrameworkTagRef{}, fmt.Errorf("the tag %q in %s needs a source, such as curated", t.Name, key)
	}

	return immutaapi.FrameworkTagRef{Name: t.Name, Source: t.Source}, nil
}

// pickTagRef returns whichever spelling of a single-tag key was used, and
// rejects a rule that uses both.
func pickTagRef(key string, terraform, api *yamlFrameworkTagRef) (*yamlFrameworkTagRef, error) {
	if terraform != nil && api != nil {
		return nil, errBothSpellings(key)
	}
	if api != nil {
		return api, nil
	}

	return terraform, nil
}

// pickTagRefs is pickTagRef for the criteria lists.
func pickTagRefs(key string, terraform, api []yamlFrameworkTagRef) ([]yamlFrameworkTagRef, error) {
	if terraform != nil && api != nil {
		return nil, errBothSpellings(key)
	}
	if api != nil {
		return api, nil
	}

	return terraform, nil
}

func errBothSpellings(key string) error {
	return fmt.Errorf("%s is given twice, in its Terraform and its Immuta API spelling", key)
}

var (
	yamlUnknownField   = regexp.MustCompile(`^(line \d+): field (\S+) not found in type \S+$`)
	yamlWrongNodeKind  = regexp.MustCompile(`^(line \d+): cannot unmarshal !!(\w+)(?: .*)? into (\S+)$`)
	yamlNodeKindNames  = map[string]string{"map": "a mapping", "seq": "a list", "str": "text", "int": "a number", "float": "a number", "bool": "a boolean"}
	yamlGoTypeMeanings = map[string]string{
		"[]provider.yamlFrameworkRule":   "a list of rules",
		"provider.yamlFrameworkRule":     "a rule",
		"*provider.yamlFrameworkTagRef":  "a tag with name and source",
		"provider.yamlFrameworkTagRef":   "a tag with name and source",
		"[]provider.yamlFrameworkTagRef": "a list of tags",
		"string":                         "text",
	}
)

// describeYAMLError rewrites yaml.v3 decoding errors in terms of the rules
// document rather than the Go types behind it, keeping the line numbers.
func describeYAMLError(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return fmt.Errorf("the rules document is not valid YAML: %w", err)
	}

	messages := make([]string, 0, len(typeErr.Errors))
	for _, message := range typeErr.Errors {
		if match := yamlUnknownField.FindStringSubmatch(message); match != nil {
			messages = append(messages, fmt.Sprintf("%s: unknown key %q", match[1], match[2]))

			continue
		}

		if match := yamlWrongNodeKind.FindStringSubmatch(message); match != nil {
			got, want := yamlNodeKindNames[match[2]], yamlGoTypeMeanings[match[3]]
			if got != "" && want != "" {
				messages = append(messages, fmt.Sprintf("%s: expected %s, got %s", match[1], want, got))

				continue
			}
		}

		messages = append(messages, strings.ReplaceAll(message, "provider.yaml", ""))
	}

	return fmt.Errorf("invalid rules document: %s", strings.Join(messages, "; "))
}
