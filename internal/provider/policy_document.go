package provider

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Documented policy types of the V2 policy API.
const (
	policyTypeData         = "data"
	policyTypeSubscription = "subscription"
)

// knownPolicyDocumentKeys are the documented top-level keys of a V2 policy.
var knownPolicyDocumentKeys = []string{
	"name", "policyKey", "type", "actions", "circumstances", "circumstanceOperator", "staged", "certification",
}

// parsePolicyDocument parses a single YAML (or JSON) document holding a
// policy into JSON-compatible values.
func parsePolicyDocument(text string) (map[string]any, error) {
	document, err := parseYAMLMapping(text, "policy document")
	if err != nil && strings.Contains(err.Error(), "single YAML document") {
		return nil, fmt.Errorf("%w; use one immuta_global_policy per policy", err)
	}

	return document, err
}

// policyDocumentFields are the fields the provider reads from a document.
type policyDocumentFields struct {
	PolicyKey string
	Name      string
	Type      string
}

// checkPolicyDocument validates the documented required fields. Unknown
// top-level keys are returned as warnings, because the reference is not
// exhaustive.
func checkPolicyDocument(document map[string]any) (policyDocumentFields, []string, error) {
	var fields policyDocumentFields

	text := func(key string) (string, error) {
		value, ok := document[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("the policy document needs a %s", key)
		}

		return value, nil
	}

	var err error
	if fields.PolicyKey, err = text("policyKey"); err != nil {
		return fields, nil, err
	}
	if fields.Name, err = text("name"); err != nil {
		return fields, nil, err
	}
	if fields.Type, err = text("type"); err != nil {
		return fields, nil, err
	}
	if fields.Type != policyTypeData && fields.Type != policyTypeSubscription {
		return fields, nil, fmt.Errorf("the policy type must be %q or %q, got %q", policyTypeData, policyTypeSubscription, fields.Type)
	}
	if document["actions"] == nil {
		return fields, nil, errors.New("the policy document needs actions")
	}

	var warnings []string
	for key := range document {
		if !slices.Contains(knownPolicyDocumentKeys, key) {
			warnings = append(warnings, fmt.Sprintf("%q is not a documented top-level policy key, so Immuta may reject or ignore it. "+
				"Documented keys: %s.", key, strings.Join(knownPolicyDocumentKeys, ", ")))
		}
	}
	slices.Sort(warnings)

	return fields, warnings, nil
}
