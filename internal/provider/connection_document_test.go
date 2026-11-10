package provider

import (
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, text string) map[string]any {
	t.Helper()

	document, err := parseYAMLMapping(text, "test document")
	if err != nil {
		t.Fatal(err)
	}

	return document
}

const snowflakeConfiguration = `
clientConfiguration:
  host: account.snowflakecomputing.com
  port: 443
  warehouse: COMPUTE
  immutaSystemDatabase: IMMUTA
policyConfiguration:
  excludeUserGroupPattern: [ADMINS]
dataObjectSync:
  enabled: true
  schedule: EVERY_24_HOURS
syncOnCreate: true
`

func TestConnectionChanges(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		technology string
		edit       func(string) string
		wantPatch  map[string]any
		wantFixed  []string
	}{
		"no change": {
			technology: "Snowflake",
			edit:       func(s string) string { return s },
			wantPatch:  map[string]any{},
		},
		"patchable setting": {
			technology: "Snowflake",
			edit:       func(s string) string { return strings.Replace(s, "COMPUTE", "LARGE", 1) },
			wantPatch:  map[string]any{"clientConfiguration": map[string]any{"warehouse": "LARGE"}},
		},
		"only the changed setting of a section is sent": {
			technology: "Snowflake",
			edit:       func(s string) string { return strings.Replace(s, "EVERY_24_HOURS", "EVERY_6_HOURS", 1) },
			wantPatch:  map[string]any{"dataObjectSync": map[string]any{"schedule": "EVERY_6_HOURS"}},
		},
		"fixed setting": {
			technology: "Snowflake",
			edit: func(s string) string {
				return strings.Replace(s, "account.snowflakecomputing.com", "other.snowflakecomputing.com", 1)
			},
			wantPatch: map[string]any{},
			wantFixed: []string{"clientConfiguration.host"},
		},
		"fixed and patchable together": {
			technology: "Snowflake",
			edit: func(s string) string {
				return strings.NewReplacer("port: 443", "port: 8443", "COMPUTE", "LARGE", "IMMUTA\n", "IMMUTA2\n").Replace(s)
			},
			wantPatch: map[string]any{"clientConfiguration": map[string]any{"warehouse": "LARGE"}},
			wantFixed: []string{"clientConfiguration.immutaSystemDatabase", "clientConfiguration.port"},
		},
		"the same setting is patchable for another technology": {
			technology: "Trino",
			edit: func(s string) string {
				return strings.Replace(s, "account.snowflakecomputing.com", "other.snowflakecomputing.com", 1)
			},
			wantPatch: map[string]any{"clientConfiguration": map[string]any{"host": "other.snowflakecomputing.com"}},
		},
		"create-only syncOnCreate is ignored": {
			technology: "Snowflake",
			edit:       func(s string) string { return strings.Replace(s, "syncOnCreate: true", "syncOnCreate: false", 1) },
			wantPatch:  map[string]any{},
		},
		"removed settings are not sent": {
			technology: "Snowflake",
			edit:       func(s string) string { return strings.Replace(s, "  warehouse: COMPUTE\n", "", 1) },
			wantPatch:  map[string]any{},
		},
		"new section": {
			technology: "Snowflake",
			edit:       func(s string) string { return s + "auditConfiguration:\n  enabled: true\n" },
			wantPatch:  map[string]any{"auditConfiguration": map[string]any{"enabled": true}},
		},
		"lists are compared whole": {
			technology: "Snowflake",
			edit:       func(s string) string { return strings.Replace(s, "[ADMINS]", "[ADMINS, OPS]", 1) },
			wantPatch:  map[string]any{"policyConfiguration": map[string]any{"excludeUserGroupPattern": []any{"ADMINS", "OPS"}}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			patch, fixed := connectionChanges(tc.technology, mustParse(t, snowflakeConfiguration), mustParse(t, tc.edit(snowflakeConfiguration)))
			if !equalJSON(patch, tc.wantPatch) {
				t.Errorf("patch = %v, want %v", patch, tc.wantPatch)
			}
			if !reflect.DeepEqual(fixed, tc.wantFixed) {
				t.Errorf("fixed = %v, want %v", fixed, tc.wantFixed)
			}
		})
	}

	// Databricks excludeGroupPattern, in its own configuration.
	before := mustParse(t, "policyConfiguration:\n  excludeGroupPattern: admins\n  impersonation: {enabled: false}\n")
	after := mustParse(t, "policyConfiguration:\n  excludeGroupPattern: ops\n  impersonation: {enabled: true}\n")
	patch, fixed := connectionChanges("Databricks", before, after)
	if !reflect.DeepEqual(fixed, []string{"policyConfiguration.excludeGroupPattern"}) {
		t.Errorf("fixed = %v, want policyConfiguration.excludeGroupPattern", fixed)
	}
	if !equalJSON(patch, map[string]any{"policyConfiguration": map[string]any{"impersonation": map[string]any{"enabled": true}}}) {
		t.Errorf("patch = %v, want only impersonation", patch)
	}
}

func TestConnectionFixedSettingsCoverEveryTechnology(t *testing.T) {
	t.Parallel()

	for _, technology := range connectionTechnologies {
		if _, ok := connectionFixedSettings[technology]; !ok {
			t.Errorf("%s has no entry in connectionFixedSettings", technology)
		}
	}
}

func TestProjectConnection(t *testing.T) {
	t.Parallel()

	configured := mustParse(t, `
clientConfiguration:
  host: trino.example.com
  ssl: true
dataObjectDefaults:
  owners:
    - {type: USER, id: 2}
`)

	cases := map[string]struct {
		remote    string
		wantDrift bool
		want      string
	}{
		"server fields and defaults are ignored": {
			remote: `{"clientConfiguration": {"host": "trino.example.com", "ssl": true, "port": 443},
				"dataObjectDefaults": {"owners": [{"type": "USER", "id": 2, "name": "Stu"}]}, "state": "ACTIVE"}`,
		},
		"a configured setting changed": {
			remote:    `{"clientConfiguration": {"host": "other.example.com", "ssl": true}, "dataObjectDefaults": {"owners": [{"type": "USER", "id": 2}]}}`,
			wantDrift: true,
			want:      `{"clientConfiguration": {"host": "other.example.com", "ssl": true}, "dataObjectDefaults": {"owners": [{"type": "USER", "id": 2}]}}`,
		},
		"a list changed length": {
			remote:    `{"clientConfiguration": {"host": "trino.example.com", "ssl": true}, "dataObjectDefaults": {"owners": []}}`,
			wantDrift: true,
			want:      `{"clientConfiguration": {"host": "trino.example.com", "ssl": true}, "dataObjectDefaults": {"owners": []}}`,
		},
		"settings Immuta does not return are assumed unchanged": {
			remote: `{"clientConfiguration": {"host": "trino.example.com"}}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			projected := projectOntoRemote(configured, remoteConnectionConfiguration(mustParse(t, tc.remote)))
			if drift := !equalJSON(projected, configured); drift != tc.wantDrift {
				t.Fatalf("drift = %t, want %t (projected %v)", drift, tc.wantDrift, projected)
			}
			if tc.wantDrift && !equalJSON(projected, mustParse(t, tc.want)) {
				t.Errorf("projected = %v, want %s", projected, tc.want)
			}
		})
	}
}

func TestRemoteConnectionConfigurationDropsTheCredential(t *testing.T) {
	t.Parallel()

	remote := remoteConnectionConfiguration(mustParse(t, `{"connectionKey": "k", "technology": "Trino", "state": "ACTIVE",
		"clientConfiguration": {"host": "h", "credential": {"authenticationType": "UsernamePassword"}}, "syncOnCreate": true}`))

	want := map[string]any{"clientConfiguration": map[string]any{"host": "h"}, "syncOnCreate": true}
	if !equalJSON(remote, want) {
		t.Errorf("remote configuration = %v, want %v", remote, want)
	}
}

func TestCheckConnectionConfiguration(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		document string
		wantErr  string
		wantWarn string
	}{
		"valid":                 {document: snowflakeConfiguration},
		"technology":            {document: "technology: Trino\n", wantErr: "set by the technology attribute"},
		"connectionKey":         {document: "connectionKey: k\n", wantErr: "set by the connection_key attribute"},
		"credential":            {document: "clientConfiguration:\n  credential: {authenticationType: Token}\n", wantErr: "sensitive credential attribute"},
		"section not a map":     {document: "dataObjectSync: true\n", wantErr: "dataObjectSync must be a mapping"},
		"syncOnCreate not bool": {document: "syncOnCreate: yes please\n", wantErr: "syncOnCreate must be true or false"},
		"undocumented key":      {document: "dataObjectSyncc: {enabled: true}\n", wantWarn: `"dataObjectSyncc"`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			warnings, err := checkConnectionConfiguration(mustParse(t, tc.document))
			switch {
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %s", err)
			}
			if tc.wantWarn != "" && (len(warnings) != 1 || !strings.Contains(warnings[0], tc.wantWarn)) {
				t.Errorf("warnings = %v, want one mentioning %s", warnings, tc.wantWarn)
			}
			if tc.wantWarn == "" && len(warnings) != 0 {
				t.Errorf("unexpected warnings: %v", warnings)
			}
		})
	}

	if err := checkConnectionCredential(mustParse(t, "token: x\n")); err == nil {
		t.Error("a credential without an authenticationType was accepted")
	}
	if err := checkConnectionCredential(mustParse(t, "authenticationType: Token\ntoken: x\n")); err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestConnectionCreateBody(t *testing.T) {
	t.Parallel()

	configuration := mustParse(t, "clientConfiguration:\n  host: h\nsyncOnCreate: true\n")
	credential := map[string]any{"authenticationType": "Token", "token": "secret"}

	body := connectionCreateBody("k", "Databricks", configuration, credential)

	want := map[string]any{
		"technology":          "Databricks",
		"connectionKey":       "k",
		"syncOnCreate":        true,
		"clientConfiguration": map[string]any{"host": "h", "credential": credential},
	}
	if !equalJSON(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
	if _, leaked := configuration["clientConfiguration"].(map[string]any)["credential"]; leaked {
		t.Error("the credential was written into the configuration document")
	}

	// A configuration without clientConfiguration still gets the credential.
	if got := connectionCreateBody("k", "Trino", map[string]any{}, credential)["clientConfiguration"]; !equalJSON(got, map[string]any{"credential": credential}) {
		t.Errorf("clientConfiguration = %v, want only the credential", got)
	}
}
