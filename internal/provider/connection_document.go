package provider

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// connectionTechnologies are the technologies documented by the connections
// API: data platforms, the Entra and Okta identity providers, and Custom,
// whose schemas are in the connections API OpenAPI definition.
var connectionTechnologies = []string{"BigQuery", "Custom", "Databricks", "Entra", "Okta", "Redshift", "Snowflake", "Teradata", "Trino"}

// connectionTechnologyCustom is the Custom technology. A Custom connection
// holds folders and securables created in Immuta rather than crawled from a
// remote platform, so it takes no credential.
const connectionTechnologyCustom = "Custom"

// connectionConfigurationKeys are the documented top-level keys of a
// connection, other than technology and connectionKey. The object sync
// filter keys are used by the Entra and Okta identity connections.
var connectionConfigurationKeys = []string{
	"clientConfiguration", "dataObjectSync", "dataObjectDefaults", "policyConfiguration", "auditConfiguration", "syncOnCreate",
	"objectSyncFilterType", "objectSyncFilterConfiguration",
}

// connectionTopLevelOnlyKeys are top-level keys that Immuta rejects inside
// clientConfiguration.
var connectionTopLevelOnlyKeys = []string{"objectSyncFilterType", "objectSyncFilterConfiguration"}

// connectionCreateOnlyKeys only apply when a connection is registered, and
// are not part of the documented update.
var connectionCreateOnlyKeys = []string{"syncOnCreate"}

// connectionFixedSettings are the settings the documented update
// (PATCH /api/connections/{connectionKey}) cannot change, by technology and
// section. Changing one means registering the connection again.
//
// The update documentation does not cover the Entra and Okta identity
// connections, so none of their settings is treated as fixed: replacing one
// deletes its crawled groups, while Immuta rejecting an update leaves the
// connection as it is.
var connectionFixedSettings = map[string]map[string][]string{
	"BigQuery":   {"clientConfiguration": {"directoryCustomerId", "immutaDataset", "location", "projectId"}},
	"Databricks": {"clientConfiguration": {"host", "immutaSystemCatalog", "port"}, "policyConfiguration": {"excludeGroupPattern"}},
	"Entra":      {},
	"Okta":       {},
	"Redshift":   {"clientConfiguration": {"host", "immutaSystemDatabase", "port"}},
	"Snowflake":  {"clientConfiguration": {"host", "immutaSystemDatabase", "port"}},
	"Teradata":   {},
	"Trino":      {},
}

// checkConnectionConfiguration validates a configuration document. Keys set
// by other attributes are errors; undocumented top-level keys are warnings.
func checkConnectionConfiguration(document map[string]any) ([]string, error) {
	for _, key := range []string{"technology", "connectionKey"} {
		if _, ok := document[key]; ok {
			return nil, fmt.Errorf("%s must not be in the configuration; it is set by the %s attribute", key, terraformName(key))
		}
	}

	if client, ok := document["clientConfiguration"]; ok {
		clientMap, isMap := client.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("clientConfiguration must be a mapping")
		}
		if _, ok := clientMap["credential"]; ok {
			return nil, fmt.Errorf("clientConfiguration.credential must not be in the configuration; set it with the sensitive credential attribute so that it is not shown in plans")
		}
		for _, key := range connectionTopLevelOnlyKeys {
			if _, ok := clientMap[key]; ok {
				return nil, fmt.Errorf("%s must be at the top level of the configuration, not in clientConfiguration", key)
			}
		}
	}

	var warnings []string
	for key, value := range document {
		if !slices.Contains(connectionConfigurationKeys, key) {
			warnings = append(warnings, fmt.Sprintf("%q is not a documented connection key, so Immuta may reject or ignore it. "+
				"Documented keys: %s.", key, strings.Join(connectionConfigurationKeys, ", ")))

			continue
		}
		if key == "syncOnCreate" {
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("syncOnCreate must be true or false")
			}

			continue
		}
		if key == "objectSyncFilterType" {
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("objectSyncFilterType must be a string, such as prefix")
			}

			continue
		}
		if _, ok := value.(map[string]any); !ok && value != nil {
			return nil, fmt.Errorf("%s must be a mapping", key)
		}
	}
	sort.Strings(warnings)

	return warnings, nil
}

// checkTechnologyConfiguration validates the settings whose format a
// technology documents.
func checkTechnologyConfiguration(technology string, document map[string]any) error {
	client, _ := document["clientConfiguration"].(map[string]any)

	if technology == "Okta" {
		if domain, ok := client["oktaDomain"]; ok {
			if value, isString := domain.(string); !isString || !isHostname(value) {
				return fmt.Errorf("clientConfiguration.oktaDomain must be the hostname only, such as dev-123456.okta.com, " +
					"without a scheme, port or path")
			}
		}
	}

	return nil
}

// isHostname reports whether value is a bare hostname, with no scheme, port,
// path or whitespace.
func isHostname(value string) bool {
	return value != "" && !strings.ContainsAny(value, ":/?#@ \t\n")
}

// checkCredentialPresence checks that a credential is set for every
// technology but Custom, which documents only an empty credential.
func checkCredentialPresence(technology string, credentialSet bool) error {
	switch {
	case technology == connectionTechnologyCustom && credentialSet:
		return fmt.Errorf("a Custom connection takes no credential; remove the credential attribute")
	case technology != connectionTechnologyCustom && !credentialSet:
		return fmt.Errorf("a %s connection needs a credential", technology)
	}

	return nil
}

// checkConnectionCredential validates a credential document.
func checkConnectionCredential(document map[string]any) error {
	if value, ok := document["authenticationType"].(string); !ok || strings.TrimSpace(value) == "" {
		return fmt.Errorf("the credential needs an authenticationType, such as UsernamePassword or Token")
	}

	return nil
}

func terraformName(apiKey string) string {
	if apiKey == "connectionKey" {
		return "connection_key"
	}

	return apiKey
}

// connectionCreateBody builds the documented registration payload.
func connectionCreateBody(key, technology string, configuration, credential map[string]any) map[string]any {
	body := deepCopy(configuration).(map[string]any)
	body["technology"] = technology
	body["connectionKey"] = key

	client, _ := body["clientConfiguration"].(map[string]any)
	if client == nil {
		client = map[string]any{}
	}
	client["credential"] = deepCopy(credential)
	body["clientConfiguration"] = client

	return body
}

// connectionChanges compares two configurations, keyed by section and
// setting. It returns the partial update for the settings that can be
// changed in place, and the settings that cannot, as section.setting.
//
// Only settings in the new configuration are considered: removing a setting
// leaves its current value in Immuta. Create-only keys are ignored.
func connectionChanges(technology string, previous, next map[string]any) (map[string]any, []string) {
	patch := map[string]any{}
	var fixed []string

	for key, value := range next {
		if slices.Contains(connectionCreateOnlyKeys, key) {
			continue
		}

		section, isSection := value.(map[string]any)
		if !isSection {
			if !equalJSON(previous[key], value) {
				patch[key] = value
			}

			continue
		}

		previousSection, _ := previous[key].(map[string]any)
		changed := map[string]any{}
		for setting, settingValue := range section {
			if equalJSON(previousSection[setting], settingValue) {
				continue
			}
			if slices.Contains(connectionFixedSettings[technology][key], setting) {
				fixed = append(fixed, key+"."+setting)

				continue
			}
			changed[setting] = settingValue
		}
		if len(changed) > 0 {
			patch[key] = changed
		}
	}

	sort.Strings(fixed)

	return patch, fixed
}

// remoteConnectionConfiguration extracts the configuration sections from a
// connection returned by Immuta, without the redacted credential.
func remoteConnectionConfiguration(raw map[string]any) map[string]any {
	configuration := map[string]any{}

	for _, key := range connectionConfigurationKeys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		configuration[key] = deepCopy(value)
	}

	if client, ok := configuration["clientConfiguration"].(map[string]any); ok {
		delete(client, "credential")
	}

	return configuration
}

// projectOntoRemote returns configured with every value replaced by the
// value Immuta returns for it, so that drift in configured settings can be
// detected while settings Immuta adds or defaults are ignored. A setting
// Immuta does not return is assumed unchanged. It is used for connections
// and data products.
func projectOntoRemote(configured, remote any) any {
	switch c := configured.(type) {
	case map[string]any:
		r, ok := remote.(map[string]any)
		if !ok {
			return remote
		}

		projected := make(map[string]any, len(c))
		for key, value := range c {
			remoteValue, present := r[key]
			if !present {
				projected[key] = value

				continue
			}
			projected[key] = projectOntoRemote(value, remoteValue)
		}

		return projected
	case []any:
		r, ok := remote.([]any)
		if !ok || len(r) != len(c) {
			return remote
		}

		projected := make([]any, len(c))
		for i := range c {
			projected[i] = projectOntoRemote(c[i], r[i])
		}

		return projected
	default:
		return remote
	}
}

// equalJSON compares two values as JSON, so that YAML integers equal the
// numbers decoded from a JSON response.
func equalJSON(a, b any) bool {
	return reflect.DeepEqual(normalizeJSON(a), normalizeJSON(b))
}

func normalizeJSON(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}

	return decoded
}

func deepCopy(value any) any {
	switch v := value.(type) {
	case map[string]any:
		copied := make(map[string]any, len(v))
		for key, item := range v {
			copied[key] = deepCopy(item)
		}

		return copied
	case []any:
		copied := make([]any, len(v))
		for i, item := range v {
			copied[i] = deepCopy(item)
		}

		return copied
	default:
		return v
	}
}

// encodeJSONDocument renders a document for state. JSON is valid YAML, so it
// compares semantically with the configured YAML.
func encodeJSONDocument(document map[string]any) (string, error) {
	encoded, err := json.Marshal(document)

	return string(encoded), err
}
