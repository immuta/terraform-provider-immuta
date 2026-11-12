package provider

import (
	"errors"
	"fmt"
	"sort"
)

// The root and request form nodes of every review flow graph have fixed IDs
// in the Request app API; every other node has a UUID chosen by its author.
const (
	reviewFlowRootNodeID = "53c03cdf-63e1-437c-9f0e-dc68b842da59"
	reviewFlowFormNodeID = "a71c93f1-c8b6-4e3c-b219-9bfa22b2d38c"
)

// parseReviewFlow parses and checks a review flow graph document.
func parseReviewFlow(text string) (map[string]any, error) {
	graph, err := parseYAMLMapping(text, "review_flow")
	if err != nil {
		return nil, err
	}
	if err := checkReviewFlow(graph); err != nil {
		return nil, err
	}

	return graph, nil
}

// checkReviewFlow checks the structure of a review flow graph, so that
// mistakes show at plan time. Immuta validates the rest, such as decision
// expressions and reviewers, when the graph is saved.
func checkReviewFlow(graph map[string]any) error {
	for key := range graph {
		if key != "nodes" && key != "edges" {
			return fmt.Errorf("review_flow: unknown key %q: a review flow graph holds only nodes and edges", key)
		}
	}
	nodes, ok := graph["nodes"].([]any)
	if !ok || len(nodes) == 0 {
		return errors.New("review_flow: nodes must be a non-empty list")
	}
	edges, ok := graph["edges"].([]any)
	if !ok {
		return errors.New("review_flow: edges must be a list")
	}

	ids := make(map[string]bool, len(nodes))
	roots := 0
	for i, item := range nodes {
		node, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("review_flow: nodes[%d] must be a mapping", i)
		}
		id, _ := node["id"].(string)
		nodeType, _ := node["type"].(string)
		if id == "" || nodeType == "" {
			return fmt.Errorf("review_flow: nodes[%d] needs a string id and type", i)
		}
		if ids[id] {
			return fmt.Errorf("review_flow: the node id %s is used more than once", id)
		}
		ids[id] = true

		switch nodeType {
		case "root":
			roots++
			if id != reviewFlowRootNodeID {
				return fmt.Errorf("review_flow: the root node id must be %s, got %s", reviewFlowRootNodeID, id)
			}
		case "form":
			if id != reviewFlowFormNodeID {
				return fmt.Errorf("review_flow: the form node id must be %s, got %s", reviewFlowFormNodeID, id)
			}
		case "decision":
			if expression, _ := node["expression"].(string); expression == "" {
				return fmt.Errorf("review_flow: the decision node %s needs an expression", id)
			}
		case "outcome":
			switch outcome, _ := node["outcome"].(string); outcome {
			case "approve", "deny", "human-review":
			default:
				return fmt.Errorf("review_flow: the outcome node %s needs an outcome of approve, deny or human-review", id)
			}
		}
	}
	if roots != 1 {
		return fmt.Errorf("review_flow: a review flow needs exactly one root node, with the id %s", reviewFlowRootNodeID)
	}

	for i, item := range edges {
		edge, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("review_flow: edges[%d] must be a mapping", i)
		}
		for _, end := range []string{"source", "target"} {
			id, _ := edge[end].(string)
			if !ids[id] {
				return fmt.Errorf("review_flow: edges[%d].%s must be the id of a node, got %v", i, end, edge[end])
			}
		}
		if branch, present := edge["branch"]; present {
			if _, ok := branch.(bool); !ok {
				return fmt.Errorf("review_flow: edges[%d].branch must be true or false", i)
			}
		}
	}

	return nil
}

// projectReviewFlow returns the configured graph with each node replaced by
// Immuta's node with the same id, projected onto the configured keys, so that
// fields Immuta adds (such as reviewer display names) and the order of nodes
// and edges never show as drift. When the nodes or edges differ, Immuta's
// graph is returned so that the drift is planned.
func projectReviewFlow(configured, remote map[string]any) map[string]any {
	configuredNodes, _ := configured["nodes"].([]any)
	remoteNodes, _ := remote["nodes"].([]any)

	remoteByID := make(map[string]any, len(remoteNodes))
	for _, item := range remoteNodes {
		if node, ok := item.(map[string]any); ok {
			if id, ok := node["id"].(string); ok {
				remoteByID[id] = node
			}
		}
	}
	if len(remoteByID) != len(configuredNodes) || len(remoteByID) != len(remoteNodes) {
		return remoteReviewFlow(remote)
	}

	nodes := make([]any, len(configuredNodes))
	for i, item := range configuredNodes {
		node, _ := item.(map[string]any)
		id, _ := node["id"].(string)
		remoteNode, ok := remoteByID[id]
		if !ok {
			return remoteReviewFlow(remote)
		}
		nodes[i] = projectOntoRemote(node, remoteNode)
	}

	edges := configured["edges"]
	if !equalJSON(reviewFlowEdgeSet(configured["edges"]), reviewFlowEdgeSet(remote["edges"])) {
		edges = remote["edges"]
	}

	return map[string]any{"nodes": nodes, "edges": edges}
}

// remoteReviewFlow keeps only the graph of a review flow, without the
// audit fields Immuta adds.
func remoteReviewFlow(remote map[string]any) map[string]any {
	return map[string]any{"nodes": deepCopy(remote["nodes"]), "edges": deepCopy(remote["edges"])}
}

// reviewFlowEdgeSet returns the edges in a canonical order. An edge without a
// branch is not the same as one with branch false.
func reviewFlowEdgeSet(value any) []string {
	edges, _ := value.([]any)
	keys := make([]string, 0, len(edges))
	for _, item := range edges {
		edge, _ := item.(map[string]any)
		keys = append(keys, fmt.Sprintf("%v|%v|%v", edge["source"], edge["target"], edge["branch"]))
	}
	sort.Strings(keys)

	return keys
}
