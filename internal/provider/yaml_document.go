package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"gopkg.in/yaml.v3"
)

// parseYAMLMapping parses a single YAML (or JSON) document holding a mapping
// into JSON-compatible values. what names the document in errors.
func parseYAMLMapping(text, what string) (map[string]any, error) {
	decoder := yaml.NewDecoder(strings.NewReader(text))

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("the %s is empty", what)
		}

		return nil, fmt.Errorf("the %s is not valid YAML: %w", what, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("the %s must contain a single YAML document", what)
	}

	normalized, err := jsonCompatible(decoded)
	if err != nil {
		return nil, fmt.Errorf("the %s: %w", what, err)
	}

	document, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("the %s must be a mapping", what)
	}

	return document, nil
}

// jsonCompatible converts decoded YAML into values encoding/json can
// marshal, rejecting mappings whose keys are not strings.
func jsonCompatible(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			converted, err := jsonCompatible(item)
			if err != nil {
				return nil, err
			}
			v[key] = converted
		}

		return v, nil
	case map[any]any:
		converted := make(map[string]any, len(v))
		for key, item := range v {
			text, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("the policy document has a non-text key %v", key)
			}
			itemValue, err := jsonCompatible(item)
			if err != nil {
				return nil, err
			}
			converted[text] = itemValue
		}

		return converted, nil
	case []any:
		for i, item := range v {
			converted, err := jsonCompatible(item)
			if err != nil {
				return nil, err
			}
			v[i] = converted
		}

		return v, nil
	default:
		return v, nil
	}
}

// parseYAMLDocumentValue parses a document for semantic comparison.
func parseYAMLDocumentValue(text string) (map[string]any, error) {
	return parseYAMLMapping(text, "document")
}

var (
	_ basetypes.StringTypable                    = yamlDocumentType{}
	_ basetypes.StringValuableWithSemanticEquals = yamlDocumentValue{}
)

// yamlDocumentType is a string holding a YAML or JSON document, such as a
// policy or a connection configuration.
// Two documents are semantically equal when they parse to the same data, so
// formatting, comments, key order and YAML versus JSON never produce a diff.
type yamlDocumentType struct {
	basetypes.StringType
}

func (t yamlDocumentType) Equal(other attr.Type) bool {
	o, ok := other.(yamlDocumentType)

	return ok && t.StringType.Equal(o.StringType)
}

func (t yamlDocumentType) String() string {
	return "yamlDocumentType"
}

func (t yamlDocumentType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return yamlDocumentValue{StringValue: in}, nil
}

func (t yamlDocumentType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	value, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}

	stringValue, ok := value.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", value)
	}

	return yamlDocumentValue{StringValue: stringValue}, nil
}

func (t yamlDocumentType) ValueType(_ context.Context) attr.Value {
	return yamlDocumentValue{}
}

// yamlDocumentValue is a value of yamlDocumentType.
type yamlDocumentValue struct {
	basetypes.StringValue
}

func (v yamlDocumentValue) Equal(other attr.Value) bool {
	o, ok := other.(yamlDocumentValue)

	return ok && v.StringValue.Equal(o.StringValue)
}

func (v yamlDocumentValue) Type(_ context.Context) attr.Type {
	return yamlDocumentType{}
}

// StringSemanticEquals reports whether two documents hold the same data.
// A document that does not parse is only equal to an identical string.
func (v yamlDocumentValue) StringSemanticEquals(_ context.Context, newValuable basetypes.StringValuable) (bool, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	newValue, ok := newValuable.(yamlDocumentValue)
	if !ok {
		diagnostics.AddError("Semantic equality check error", fmt.Sprintf("expected a YAML document value, got %T", newValuable))

		return false, diagnostics
	}

	if v.ValueString() == newValue.ValueString() {
		return true, diagnostics
	}

	current, err := parseYAMLDocumentValue(v.ValueString())
	if err != nil {
		return false, diagnostics
	}
	proposed, err := parseYAMLDocumentValue(newValue.ValueString())
	if err != nil {
		return false, diagnostics
	}

	return reflect.DeepEqual(current, proposed), diagnostics
}

var _ planmodifier.String = keepEquivalentYAMLDocument{}

// keepEquivalentYAMLDocument plans the stored document when the configured
// one holds the same data. The framework only applies semantic equality to
// values returned by Create, Read and Update, so without this a reformatted
// file would plan an update. Terraform accepts the prior value in place of
// the configured one for exactly this purpose.
type keepEquivalentYAMLDocument struct{}

func (m keepEquivalentYAMLDocument) Description(_ context.Context) string {
	return "Ignores changes to formatting, comments and key order in the document."
}

func (m keepEquivalentYAMLDocument) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m keepEquivalentYAMLDocument) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}

	state := yamlDocumentValue{StringValue: req.StateValue}
	equal, diags := state.StringSemanticEquals(ctx, yamlDocumentValue{StringValue: req.PlanValue})
	resp.Diagnostics.Append(diags...)

	if equal {
		resp.PlanValue = req.StateValue
	}
}
