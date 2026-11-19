package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Dynamic review flow request types, the {requestType} of the review flow
// endpoints.
const (
	ReviewFlowDataAccess       = "DATA_ACCESS"
	ReviewFlowMaskingException = "MASKING_EXCEPTION"
)

// ReviewFlowRequestTypes are the documented request types.
var ReviewFlowRequestTypes = []string{ReviewFlowDataAccess, ReviewFlowMaskingException}

// reviewFlowFields are the fields that hold each request type's review flow
// in GET /api/asset/{id}/review-flows and GET /api/data-product/{id}.
var reviewFlowFields = map[string]string{
	ReviewFlowDataAccess:       "dataAccessReviewFlow",
	ReviewFlowMaskingException: "maskingExceptionReviewFlow",
}

// ReviewFlow is a dynamic review flow graph as Immuta returns it.
type ReviewFlow struct {
	// Graph holds the nodes and edges.
	Graph map[string]any
	// InheritedFrom is the id of the ancestor asset the graph is inherited
	// from, empty when the graph is the asset's own.
	InheritedFrom string
}

// PutAssetReviewFlow saves the review flow graph of an asset for a request
// type, overriding the one it inherits, with
// PUT /api/asset/{id}/review-flows/{requestType}.
func (c *Client) PutAssetReviewFlow(ctx context.Context, assetID, requestType string, graph map[string]any) error {
	if err := validateReviewFlowRef("an asset", assetID, requestType); err != nil {
		return err
	}

	_, err := c.requestAppDo(ctx, request{
		method:   http.MethodPut,
		segments: []string{"api", "asset", assetID, "review-flows", requestType},
		body:     graph,
	})

	return err
}

// GetAssetReviewFlow reads the review flow of an asset for a request type
// with the documented GET /api/asset/{id}/review-flows. It returns nil when
// the asset has no review flow graph for the request type, for example a
// legacy review flow. It returns an error matching ErrNotFound when the asset
// does not exist.
func (c *Client) GetAssetReviewFlow(ctx context.Context, assetID, requestType string) (*ReviewFlow, error) {
	if err := validateReviewFlowRef("an asset", assetID, requestType); err != nil {
		return nil, err
	}

	response, err := c.requestAppDo(ctx, request{
		method:   http.MethodGet,
		segments: []string{"api", "asset", assetID, "review-flows"},
	})
	if err != nil {
		return nil, err
	}

	return decodeReviewFlowField(response, requestType, "asset review flows")
}

// DeleteAssetReviewFlow removes the review flow override of an asset for a
// request type, with DELETE /api/asset/{id}/review-flows/{requestType}. The
// asset then inherits the nearest applicable review flow again.
func (c *Client) DeleteAssetReviewFlow(ctx context.Context, assetID, requestType string) error {
	if err := validateReviewFlowRef("an asset", assetID, requestType); err != nil {
		return err
	}

	_, err := c.requestAppDo(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"api", "asset", assetID, "review-flows", requestType},
	})

	return err
}

// PutDataProductReviewFlow saves the review flow graph of a data product for
// a request type with the documented
// PUT /api/data-product/{id}/review-flows/{requestType}.
func (c *Client) PutDataProductReviewFlow(ctx context.Context, dataProductID, requestType string, graph map[string]any) error {
	if err := validateReviewFlowRef("a data product", dataProductID, requestType); err != nil {
		return err
	}

	_, err := c.requestAppDo(ctx, request{
		method:   http.MethodPut,
		segments: dataProductPath(dataProductID, "review-flows", requestType),
		body:     graph,
	})

	return err
}

// GetDataProductReviewFlow reads the review flow of a data product for a
// request type from the documented GET /api/data-product/{id}, which only
// returns it when it is a review flow graph. It returns nil otherwise, and an
// error matching ErrNotFound when the data product does not exist.
func (c *Client) GetDataProductReviewFlow(ctx context.Context, dataProductID, requestType string) (*ReviewFlow, error) {
	if err := validateReviewFlowRef("a data product", dataProductID, requestType); err != nil {
		return nil, err
	}

	response, err := c.requestAppDo(ctx, request{
		method:   http.MethodGet,
		segments: dataProductPath(dataProductID),
	})
	if err != nil {
		return nil, err
	}

	return decodeReviewFlowField(response, requestType, "data product")
}

func validateReviewFlowRef(what, id, requestType string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s id is required", what)
	}
	if _, ok := reviewFlowFields[requestType]; !ok {
		return fmt.Errorf("a review flow request type must be %s or %s, got %q", ReviewFlowDataAccess, ReviewFlowMaskingException, requestType)
	}

	return nil
}

// decodeReviewFlowField reads one request type's review flow from a response
// holding dataAccessReviewFlow and maskingExceptionReviewFlow. A missing or
// null field, or a legacy flow without nodes, is no graph.
func decodeReviewFlowField(body []byte, requestType, what string) (*ReviewFlow, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var decoded map[string]any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, fmt.Errorf("decoding %s response: %w", what, err)
	}

	flow, _ := decoded[reviewFlowFields[requestType]].(map[string]any)
	if flow == nil {
		return nil, nil
	}
	if _, isGraph := flow["nodes"]; !isGraph {
		return nil, nil
	}

	nodes, nodesOK := flow["nodes"].([]any)
	edges, edgesOK := flow["edges"].([]any)
	if !nodesOK || !edgesOK {
		return nil, errors.New("decoding " + what + " response: a review flow graph needs nodes and edges lists")
	}

	result := &ReviewFlow{Graph: map[string]any{"nodes": nodes, "edges": edges}}
	if inherited, ok := flow["inheritedFrom"].(map[string]any); ok {
		id, err := flexibleIDValue(inherited["id"])
		if err != nil || id == "" {
			return nil, errors.New("decoding " + what + " response: inheritedFrom has no id")
		}
		result.InheritedFrom = id
	}

	return result, nil
}

// flexibleIDValue reads an id decoded as a string or a number.
func flexibleIDValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprintf("%.0f", v), nil
	default:
		return "", fmt.Errorf("unexpected id %v", value)
	}
}
