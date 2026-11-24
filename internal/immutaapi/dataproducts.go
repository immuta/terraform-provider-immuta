package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// dataProductPathPrefix is the documented data product path, relative to the
// Request app (formerly Marketplace) API base URL, such as
// https://na.api.immutacloud.com/marketplace. Data product calls go to the
// Request app client configured with WithRequestApp, never the tenant host.
var dataProductPathPrefix = []string{"api", "data-product"}

// DataProduct holds the documented data product fields the provider manages.
type DataProduct struct {
	ID          string
	Name        string
	Description *string
	Domains     []string
	Suspended   bool
	Metadata    *DataProductMetadata
}

// SubjectMatterExpertID returns the global user ID of the subject matter
// expert, or an empty string when Immuta returned none.
func (p *DataProduct) SubjectMatterExpertID() string {
	if p.Metadata == nil {
		return ""
	}

	return p.Metadata.SubjectMatterExpert.ID
}

// DataProductMetadata is the documented data product metadata object.
type DataProductMetadata struct {
	SubjectMatterExpert DataProductSubjectMatterExpert `json:"subjectMatterExpert"`
}

// DataProductSubjectMatterExpert identifies the subject matter expert by
// Immuta global user ID.
type DataProductSubjectMatterExpert struct {
	ID string `json:"id"`
}

// DataProductDataSource is a data source reference, as documented.
type DataProductDataSource struct {
	SourceID string `json:"sourceId"`
}

// CreateDataProductRequest is the documented POST /api/data-product body,
// without the deprecated policies and requestFormId, and without review
// flows, which the provider does not manage. Immuta requires both request
// form ids when a data product is created without review-flow graphs.
type CreateDataProductRequest struct {
	ID                            string                  `json:"id,omitempty"`
	Name                          string                  `json:"name"`
	Description                   *string                 `json:"description,omitempty"`
	Metadata                      DataProductMetadata     `json:"metadata"`
	Domains                       []string                `json:"domains"`
	DataSources                   []DataProductDataSource `json:"dataSources"`
	DataAccessRequestFormID       string                  `json:"dataAccessRequestFormId,omitempty"`
	MaskingExceptionRequestFormID string                  `json:"maskingExceptionRequestFormId,omitempty"`
}

// UpdateDataProductRequest is the documented PUT /api/data-product/{id}
// body. The provider always sends every field it manages, so Description has
// no omitempty: null clears it. The request form ids are only sent when set:
// Immuta does not return them, and leaves them unchanged when omitted.
type UpdateDataProductRequest struct {
	Name                          string              `json:"name"`
	Description                   *string             `json:"description"`
	Metadata                      DataProductMetadata `json:"metadata"`
	Domains                       []string            `json:"domains"`
	Suspended                     bool                `json:"suspended"`
	DataAccessRequestFormID       string              `json:"dataAccessRequestFormId,omitempty"`
	MaskingExceptionRequestFormID string              `json:"maskingExceptionRequestFormId,omitempty"`
}

// requestAppDo performs a Request app API request.
func (c *Client) requestAppDo(ctx context.Context, r request) ([]byte, error) {
	requestApp, err := c.requestAppClient()
	if err != nil {
		return nil, err
	}

	return requestApp.do(ctx, r)
}

func dataProductPath(segments ...string) []string {
	return slices.Concat(dataProductPathPrefix, segments)
}

// DataProductSources turns source ids into the documented dataSources list.
func DataProductSources(sourceIDs []string) []DataProductDataSource {
	dataSources := make([]DataProductDataSource, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		dataSources = append(dataSources, DataProductDataSource{SourceID: sourceID})
	}

	return dataSources
}

// CreateDataProduct adds a data product with POST /api/data-product.
func (c *Client) CreateDataProduct(ctx context.Context, req CreateDataProductRequest) (*DataProduct, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("a data product name is required")
	}
	if req.Domains == nil {
		req.Domains = []string{}
	}
	if req.DataSources == nil {
		req.DataSources = []DataProductDataSource{}
	}

	response, err := c.requestAppDo(ctx, request{
		method:   http.MethodPost,
		segments: dataProductPath(),
		body:     req,
	})
	if err != nil {
		return nil, err
	}

	return decodeDataProduct(response)
}

// GetDataProduct reads a data product with GET /api/data-product/{id}. It
// returns an error matching ErrNotFound when the data product does not exist.
func (c *Client) GetDataProduct(ctx context.Context, id string) (*DataProduct, error) {
	if err := validateDataProductID(id); err != nil {
		return nil, err
	}

	response, err := c.requestAppDo(ctx, request{
		method:   http.MethodGet,
		segments: dataProductPath(id),
	})
	if err != nil {
		return nil, err
	}

	return decodeDataProduct(response)
}

// UpdateDataProduct updates a data product with PUT /api/data-product/{id}.
// Its data sources are changed with SetDataProductDataSources instead.
func (c *Client) UpdateDataProduct(ctx context.Context, id string, req UpdateDataProductRequest) error {
	if err := validateDataProductID(id); err != nil {
		return err
	}
	if req.Domains == nil {
		req.Domains = []string{}
	}

	_, err := c.requestAppDo(ctx, request{
		method:   http.MethodPut,
		segments: dataProductPath(id),
		body:     req,
	})

	return err
}

// DeleteDataProduct deletes a data product with DELETE /api/data-product/{id}.
func (c *Client) DeleteDataProduct(ctx context.Context, id string) error {
	if err := validateDataProductID(id); err != nil {
		return err
	}

	_, err := c.requestAppDo(ctx, request{
		method:   http.MethodDelete,
		segments: dataProductPath(id),
	})

	return err
}

// SetDataProductDataSources replaces the data sources of a data product with
// PUT /api/data-product/{id}/datasources.
func (c *Client) SetDataProductDataSources(ctx context.Context, id string, sourceIDs []string) error {
	if err := validateDataProductID(id); err != nil {
		return err
	}

	_, err := c.requestAppDo(ctx, request{
		method:   http.MethodPut,
		segments: dataProductPath(id, "datasources"),
		body:     map[string]any{"dataSources": DataProductSources(sourceIDs)},
	})

	return err
}

// GetDataProductDataSources returns the source ids of every data source in a
// data product, with GET /api/data-product/{id}/datasources/all.
func (c *Client) GetDataProductDataSources(ctx context.Context, id string) ([]string, error) {
	if err := validateDataProductID(id); err != nil {
		return nil, err
	}

	response, err := c.requestAppDo(ctx, request{
		method:   http.MethodGet,
		segments: dataProductPath(id, "datasources", "all"),
	})
	if err != nil {
		return nil, err
	}

	var decoded struct {
		DataSources []struct {
			SourceID json.RawMessage `json:"sourceId"`
		} `json:"dataSources"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(response), &decoded); err != nil {
		return nil, fmt.Errorf("decoding data product data sources: %w", err)
	}

	sourceIDs := make([]string, 0, len(decoded.DataSources))
	for _, dataSource := range decoded.DataSources {
		// Documented as a string; numbers are accepted too.
		sourceID, err := flexibleID(dataSource.SourceID)
		if err != nil || sourceID == "" {
			return nil, errors.New("decoding data product data sources: a data source has no sourceId")
		}
		sourceIDs = append(sourceIDs, sourceID)
	}

	return sourceIDs, nil
}

// validateDataProductID guards against requests to /api/data-product/ with
// an empty id, which would otherwise hit the search endpoint.
func validateDataProductID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("a data product id is required")
	}

	return nil
}

func decodeDataProduct(body []byte) (*DataProduct, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errEmptyBody
	}

	var decoded struct {
		ID          json.RawMessage      `json:"id"`
		Name        string               `json:"name"`
		Description *string              `json:"description"`
		Domains     []string             `json:"domains"`
		Suspended   bool                 `json:"suspended"`
		Metadata    *DataProductMetadata `json:"metadata"`
	}
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, fmt.Errorf("decoding data product response: %w", err)
	}

	id, err := flexibleID(decoded.ID)
	if err != nil || id == "" {
		return nil, errors.New("decoding data product response: the API response did not contain a data product id")
	}

	return &DataProduct{
		ID:          id,
		Name:        decoded.Name,
		Description: decoded.Description,
		Domains:     decoded.Domains,
		Suspended:   decoded.Suspended,
		Metadata:    decoded.Metadata,
	}, nil
}
