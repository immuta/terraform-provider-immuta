package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRequestFormID = "3f2a1b0c-9d8e-4f7a-8b6c-5d4e3f2a1b00"

// exampleReviewFlowFile is the path of an example review flow file.
func exampleReviewFlowFile(t *testing.T, resourceType, name string) string {
	t.Helper()

	file, err := filepath.Abs(filepath.Join("..", "..", "examples", "resources", resourceType, "review_flows", name))
	if err != nil {
		t.Fatal(err)
	}

	return file
}

// analystsReviewFlow is the asset example, rendered as templatefile() would.
func analystsReviewFlow(t *testing.T) string {
	t.Helper()

	text, err := os.ReadFile(exampleReviewFlowFile(t, "immuta_asset_review_flow", "analysts.yaml.tftpl"))
	if err != nil {
		t.Fatal(err)
	}

	return strings.ReplaceAll(string(text), "${request_form_id}", testRequestFormID)
}

func TestParseReviewFlowExamples(t *testing.T) {
	t.Parallel()

	autoApprove, err := os.ReadFile(exampleReviewFlowFile(t, "immuta_data_product_review_flow", "auto_approve.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	for name, text := range map[string]string{"analysts": analystsReviewFlow(t), "auto approve": string(autoApprove)} {
		graph, err := parseReviewFlow(text)
		if err != nil {
			t.Errorf("%s: %s", name, err)

			continue
		}
		if len(graph) != 2 {
			t.Errorf("%s: graph keys = %v, want nodes and edges", name, graph)
		}
	}
}

func TestParseReviewFlowRejectsInvalidGraphs(t *testing.T) {
	t.Parallel()

	const (
		root     = "{id: " + reviewFlowRootNodeID + ", type: root}"
		approve  = "{id: 9b2e4c7a-3d5f-4e8a-b1c6-7d8e9f0a1b22, type: outcome, outcome: approve}"
		edge     = "{source: " + reviewFlowRootNodeID + ", target: 9b2e4c7a-3d5f-4e8a-b1c6-7d8e9f0a1b22}"
		validSet = "nodes: [" + root + ", " + approve + "]\n"
	)

	for name, tc := range map[string]struct{ text, want string }{
		"not a mapping":        {"[]", "review_flow"},
		"unknown key":          {validSet + "edges: [" + edge + "]\nupdatedAt: now\n", `unknown key "updatedAt"`},
		"no nodes":             {"nodes: []\nedges: []\n", "nodes must be a non-empty list"},
		"no edges":             {validSet, "edges must be a list"},
		"node without type":    {"nodes: [{id: x}]\nedges: []\n", "needs a string id and type"},
		"wrong root id":        {"nodes: [{id: x, type: root}]\nedges: []\n", "root node id must be " + reviewFlowRootNodeID},
		"no root":              {"nodes: [" + approve + "]\nedges: []\n", "exactly one root node"},
		"wrong form id":        {"nodes: [" + root + ", {id: x, type: form}]\nedges: []\n", "form node id must be " + reviewFlowFormNodeID},
		"duplicate id":         {"nodes: [" + root + ", " + root + "]\nedges: []\n", "used more than once"},
		"decision without exp": {"nodes: [" + root + ", {id: d, type: decision}]\nedges: []\n", "decision node d needs an expression"},
		"unknown outcome":      {"nodes: [" + root + ", {id: o, type: outcome, outcome: maybe}]\nedges: []\n", "approve, deny or human-review"},
		"edge to no node":      {validSet + "edges: [{source: " + reviewFlowRootNodeID + ", target: missing}]\n", "edges[0].target must be the id of a node"},
		"branch not boolean":   {validSet + "edges: [{source: " + reviewFlowRootNodeID + ", target: 9b2e4c7a-3d5f-4e8a-b1c6-7d8e9f0a1b22, branch: yes please}]\n", "branch must be true or false"},
	} {
		_, err := parseReviewFlow(tc.text)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want it to contain %q", name, err, tc.want)
		}
	}
}

func TestProjectReviewFlow(t *testing.T) {
	t.Parallel()

	configured, err := parseReviewFlow(analystsReviewFlow(t))
	if err != nil {
		t.Fatal(err)
	}

	// remote returns the configured graph as Immuta would: extra fields on
	// the graph and its nodes, and the edges in another order.
	remote := func(change func(graph map[string]any)) map[string]any {
		graph := deepCopy(configured).(map[string]any)
		graph["updatedAt"] = "2026-11-11T00:00:00Z"
		for _, node := range graph["nodes"].([]any) {
			node.(map[string]any)["position"] = map[string]any{"x": 1}
		}
		edges := graph["edges"].([]any)
		edges[0], edges[3] = edges[3], edges[0]
		if change != nil {
			change(graph)
		}

		return graph
	}
	node := func(graph map[string]any, i int) map[string]any { return graph["nodes"].([]any)[i].(map[string]any) }

	if got := projectReviewFlow(configured, remote(nil)); !equalJSON(got, configured) {
		t.Errorf("an unchanged graph projects to %v, want the configured graph", got)
	}

	for name, change := range map[string]func(map[string]any){
		"changed expression": func(g map[string]any) { node(g, 2)["expression"] = "true" },
		"extra node": func(g map[string]any) {
			g["nodes"] = append(g["nodes"].([]any), map[string]any{"id": "extra", "type": "outcome", "outcome": "deny"})
		},
		"replaced node":   func(g map[string]any) { node(g, 4)["id"] = "other" },
		"flipped branch":  func(g map[string]any) { g["edges"].([]any)[1].(map[string]any)["branch"] = true },
		"missing edge":    func(g map[string]any) { g["edges"] = g["edges"].([]any)[1:] },
		"no branch value": func(g map[string]any) { delete(g["edges"].([]any)[0].(map[string]any), "branch") },
	} {
		got := projectReviewFlow(configured, remote(change))
		if equalJSON(got, configured) {
			t.Errorf("%s: no drift was recorded", name)
		}
		if _, audit := got["updatedAt"]; audit {
			t.Errorf("%s: the audit fields were recorded: %v", name, got)
		}
	}
}
