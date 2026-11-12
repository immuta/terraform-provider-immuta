package testutil

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// The fixed root node id of every review flow graph.
const reviewFlowRootNodeID = "53c03cdf-63e1-437c-9f0e-dc68b842da59"

var reviewFlowFields = map[string]string{
	"DATA_ACCESS":       "dataAccessReviewFlow",
	"MASKING_EXCEPTION": "maskingExceptionReviewFlow",
}

// storedAsset holds an asset's own review flow graphs, and those it
// inherits, by request type.
type storedAsset struct {
	flows     map[string]map[string]any
	inherited map[string]inheritedFlow
}

type inheritedFlow struct {
	fromID string
	graph  map[string]any
}

// AddAsset makes an asset known to the review flow endpoints.
func (s *DataProductServer) AddAsset(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.assets == nil {
		s.assets = map[string]*storedAsset{}
	}
	s.assets[id] = &storedAsset{flows: map[string]map[string]any{}, inherited: map[string]inheritedFlow{}}
}

// SetInheritedAssetReviewFlow makes an asset inherit a review flow from an
// ancestor, shown when it has no flow of its own.
func (s *DataProductServer) SetInheritedAssetReviewFlow(assetID, requestType, fromID string, graph map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.assets[assetID].inherited[requestType] = inheritedFlow{fromID: fromID, graph: graph}
}

// AssetReviewFlow returns an asset's own review flow graph for a request
// type, nil when it has none.
func (s *DataProductServer) AssetReviewFlow(assetID, requestType string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	if asset, ok := s.assets[assetID]; ok {
		return asset.flows[requestType]
	}

	return nil
}

// SetAssetReviewFlow changes or, with nil, removes an asset's own review
// flow out of band, as a change in the Request app would.
func (s *DataProductServer) SetAssetReviewFlow(assetID, requestType string, graph map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if graph == nil {
		delete(s.assets[assetID].flows, requestType)

		return
	}
	s.assets[assetID].flows[requestType] = graph
}

// DataProductReviewFlow returns the review flow graph saved for a data
// product and request type with the review flow endpoint.
func (s *DataProductServer) DataProductReviewFlow(id, requestType string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	if product, ok := s.products[id]; ok {
		return product.reviewFlows[requestType]
	}

	return nil
}

// handleAssetReviewFlows serves /api/asset/{id}/review-flows[/{requestType}].
func (s *DataProductServer) handleAssetReviewFlows(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) < 2 || rest[1] != "review-flows" || len(rest) > 3 {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

		return
	}
	id, _ := url.PathUnescape(rest[0])

	var graph map[string]any
	if r.Method == http.MethodPut {
		var ok bool
		if graph, ok = decodeReviewFlowGraph(w, r); !ok {
			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	asset, ok := s.assets[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Asset not found"})

		return
	}

	if len(rest) == 2 {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})

			return
		}
		response := map[string]any{}
		for requestType, field := range reviewFlowFields {
			if own, ok := asset.flows[requestType]; ok {
				response[field] = renderReviewFlowGraph(own)
			} else if inherited, ok := asset.inherited[requestType]; ok {
				rendered := renderReviewFlowGraph(inherited.graph)
				rendered["inheritedFrom"] = map[string]any{"id": inherited.fromID, "name": "Parent"}
				response[field] = rendered
			}
		}
		writeJSON(w, http.StatusOK, response)

		return
	}

	requestType := rest[2]
	if _, known := reviewFlowFields[requestType]; !known {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request type"})

		return
	}
	switch r.Method {
	case http.MethodPut:
		asset.flows[requestType] = graph
		writeJSON(w, http.StatusOK, renderReviewFlowGraph(graph))
	case http.MethodDelete:
		if _, ok := asset.flows[requestType]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "No review flow override"})

			return
		}
		delete(asset.flows, requestType)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "unknown endpoint"})
	}
}

// putDataProductReviewFlow serves PUT /api/data-product/{id}/review-flows/{requestType}.
func (s *DataProductServer) putDataProductReviewFlow(w http.ResponseWriter, r *http.Request, id, requestType string) {
	graph, ok := decodeReviewFlowGraph(w, r)
	if !ok {
		return
	}
	if _, known := reviewFlowFields[requestType]; !known {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid request type"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	product, ok := s.products[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Data product not found"})

		return
	}
	if product.reviewFlows == nil {
		product.reviewFlows = map[string]map[string]any{}
	}
	product.reviewFlows[requestType] = graph

	writeJSON(w, http.StatusOK, renderReviewFlowGraph(graph))
}

// decodeReviewFlowGraph applies the documented body rules: only nodes and
// edges, and a root node with the fixed id.
func decodeReviewFlowGraph(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var graph map[string]any
	if err := json.NewDecoder(r.Body).Decode(&graph); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})

		return nil, false
	}
	nodes, nodesOK := graph["nodes"].([]any)
	_, edgesOK := graph["edges"].([]any)
	if len(graph) != 2 || !nodesOK || !edgesOK {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "a review flow graph holds only nodes and edges"})

		return nil, false
	}
	root := false
	for _, item := range nodes {
		if node, ok := item.(map[string]any); ok && node["type"] == "root" && node["id"] == reviewFlowRootNodeID {
			root = true
		}
	}
	if !root {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "the graph needs the root node"})

		return nil, false
	}

	return graph, true
}

// renderReviewFlowGraph returns a graph as the API does: with audit fields
// the provider ignores, an extra field on each node, and the edges in
// reverse order.
func renderReviewFlowGraph(graph map[string]any) map[string]any {
	encoded, _ := json.Marshal(graph)

	var rendered map[string]any
	_ = json.Unmarshal(encoded, &rendered)

	for _, item := range rendered["nodes"].([]any) {
		if node, ok := item.(map[string]any); ok {
			node["position"] = map[string]any{"x": 0, "y": 0}
		}
	}
	edges := rendered["edges"].([]any)
	for i, j := 0, len(edges)-1; i < j; i, j = i+1, j-1 {
		edges[i], edges[j] = edges[j], edges[i]
	}
	rendered["updatedAt"] = "2026-11-11T00:00:00Z"
	rendered["updatedBy"] = map[string]any{"id": "5b0c1f0e-1111-2222-3333-444455556666"}

	return rendered
}
