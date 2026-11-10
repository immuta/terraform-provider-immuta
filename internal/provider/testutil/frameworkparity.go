package testutil

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/immuta/terraform-provider-immuta/internal/immutaapi"
)

// NormalizeAPIFramework reduces an API framework to the fields the provider
// owns. Tags and rules are rendered as sorted strings so that order never
// matters. created_by and created_at are compared as well, because they are
// read straight from the API.
func NormalizeAPIFramework(framework *immutaapi.Framework) map[string]any {
	tags := make([]string, 0, len(framework.Tags))
	for _, tag := range framework.Tags {
		sensitivities := make([]string, 0, len(tag.Sensitivities))
		for _, sensitivity := range tag.Sensitivities {
			sensitivities = append(sensitivities, fmt.Sprintf("%s=%d", sensitivity.Dimension, sensitivity.Sensitivity))
		}
		tags = append(tags, frameworkTagKey(tag.Name, tag.Source, sensitivities))
	}
	sort.Strings(tags)

	rules := make([]string, 0, len(framework.Rules))
	for _, rule := range framework.Rules {
		rules = append(rules, frameworkRuleKey(
			rule.Name,
			rule.ClassificationTag.Name+"|"+rule.ClassificationTag.Source,
			apiTagRefKeys(rule.ColumnTags),
			apiTagRefKeys(rule.NeighborColumnTags),
			apiTagRefKeys(rule.TableTags),
		))
	}
	sort.Strings(rules)

	createdBy := ""
	if framework.CreatedBy != nil {
		createdBy = strconv.FormatInt(*framework.CreatedBy, 10)
	}

	return map[string]any{
		"id":          emptyToNil(framework.ID),
		"version":     emptyToNil(framework.Version),
		"short_name":  emptyToNil(framework.ShortName),
		"name":        emptyToNil(framework.Name),
		"description": emptyToNil(framework.Description),
		"active":      framework.Active,
		"created_by":  emptyToNil(createdBy),
		"created_at":  emptyToNil(framework.CreatedAt),
		"tags":        tags,
		"rules":       rules,
	}
}

// NormalizeStateFramework reduces Terraform state attributes to the same
// shape as NormalizeAPIFramework.
func NormalizeStateFramework(attributes map[string]string) map[string]any {
	tags := []string{}
	for _, prefix := range stateIndexes(attributes, "tags") {
		sensitivities := []string{}
		for _, sensitivity := range stateIndexes(attributes, prefix+".sensitivities") {
			sensitivities = append(sensitivities, attributes[sensitivity+".dimension"]+"="+attributes[sensitivity+".sensitivity"])
		}
		tags = append(tags, frameworkTagKey(attributes[prefix+".name"], attributes[prefix+".source"], sensitivities))
	}
	sort.Strings(tags)

	rules := []string{}
	for _, prefix := range stateIndexes(attributes, "rules") {
		rules = append(rules, frameworkRuleKey(
			attributes[prefix+".name"],
			attributes[prefix+".classification_tag.name"]+"|"+attributes[prefix+".classification_tag.source"],
			stateTagRefKeys(attributes, prefix+".column_tags"),
			stateTagRefKeys(attributes, prefix+".neighbor_column_tags"),
			stateTagRefKeys(attributes, prefix+".table_tags"),
		))
	}
	sort.Strings(rules)

	normalized := map[string]any{
		"active": attributes["active"] == "true",
		"tags":   tags,
		"rules":  rules,
	}
	for _, key := range []string{"id", "version", "short_name", "name", "description", "created_by", "created_at"} {
		normalized[key] = emptyToNil(attributes[key])
	}

	return normalized
}

// FrameworkParityError compares Terraform state with the Immuta API and
// returns an error describing any difference.
func FrameworkParityError(framework *immutaapi.Framework, attributes map[string]string) error {
	api := NormalizeAPIFramework(framework)
	state := NormalizeStateFramework(attributes)

	if !reflect.DeepEqual(api, state) {
		return fmt.Errorf("Terraform state and the Immuta API disagree:\n  api:   %s\n  state: %s", format(api), format(state))
	}

	return nil
}

func frameworkTagKey(name, source string, sensitivities []string) string {
	sort.Strings(sensitivities)

	return fmt.Sprintf("%s|%s|[%s]", name, source, strings.Join(sensitivities, ","))
}

func frameworkRuleKey(name, classificationTag string, columnTags, neighborColumnTags, tableTags []string) string {
	for _, refs := range [][]string{columnTags, neighborColumnTags, tableTags} {
		sort.Strings(refs)
	}

	return fmt.Sprintf("%s|%s|column[%s]|neighbor[%s]|table[%s]", name, classificationTag,
		strings.Join(columnTags, ","), strings.Join(neighborColumnTags, ","), strings.Join(tableTags, ","))
}

func apiTagRefKeys(refs []immutaapi.FrameworkTagRef) []string {
	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Name+"|"+ref.Source)
	}

	return keys
}

func stateTagRefKeys(attributes map[string]string, name string) []string {
	keys := []string{}
	for _, prefix := range stateIndexes(attributes, name) {
		keys = append(keys, attributes[prefix+".name"]+"|"+attributes[prefix+".source"])
	}

	return keys
}

// stateIndexes returns the attribute prefixes of every element of a nested
// collection in flattened Terraform state, such as rules.0 and rules.1.
func stateIndexes(attributes map[string]string, name string) []string {
	count, err := strconv.Atoi(attributes[name+".#"])
	if err != nil || count == 0 {
		return nil
	}

	seen := map[string]bool{}
	for key := range attributes {
		rest, ok := strings.CutPrefix(key, name+".")
		if !ok || rest == "#" {
			continue
		}
		index, _, _ := strings.Cut(rest, ".")
		seen[name+"."+index] = true
	}

	prefixes := make([]string, 0, len(seen))
	for prefix := range seen {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)

	return prefixes
}
