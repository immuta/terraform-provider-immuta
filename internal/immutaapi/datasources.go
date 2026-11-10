package immutaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// dataSourceSearchPageSize is the page size used to search data sources.
const dataSourceSearchPageSize = 100

// DataSource holds the documented data source fields the provider uses.
type DataSource struct {
	ID   string
	Name string
	// ConnectionKey is the connection the data source comes from, if any.
	ConnectionKey string
	// NamePath is the names of the data objects down to the data source,
	// for example database, schema and table.
	NamePath []string
	// DataObjectID is the connections API data object of the data source.
	DataObjectID string
}

// SearchDataSourcesOptions are the documented GET /dataSource filters the
// provider uses. At least one is required.
type SearchDataSourcesOptions struct {
	// SearchText matches data source names partially.
	SearchText    string
	DataSourceIDs []string
	DomainID      string
}

// GetDataSourceByName reads a data source by its exact name, with the
// documented GET /dataSource/name/{dataSourceName}. It returns an error
// matching ErrNotFound when no data source has the name.
func (c *Client) GetDataSourceByName(ctx context.Context, name string) (*DataSource, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("a data source name is required")
	}

	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		segments: []string{"dataSource", "name", name},
	})
	if err != nil {
		return nil, err
	}

	var raw dataSourcePayload
	if err := json.Unmarshal(bytes.TrimSpace(body), &raw); err != nil {
		return nil, fmt.Errorf("decoding data source: %w", err)
	}

	return raw.dataSource()
}

// SearchDataSources returns every data source matching the options, with the
// documented GET /dataSource, following its pages.
func (c *Client) SearchDataSources(ctx context.Context, opts SearchDataSourcesOptions) ([]DataSource, error) {
	if opts.SearchText == "" && len(opts.DataSourceIDs) == 0 && opts.DomainID == "" {
		return nil, errors.New("a data source search needs a filter")
	}

	var dataSources []DataSource
	for offset := 0; ; {
		query := url.Values{}
		query.Set("offset", strconv.Itoa(offset))
		query.Set("size", strconv.Itoa(dataSourceSearchPageSize))
		if opts.SearchText != "" {
			query.Set("searchText", opts.SearchText)
			query.Set("nameOnly", "true")
		}
		for _, id := range opts.DataSourceIDs {
			query.Add("dataSourceIds", id)
		}
		if opts.DomainID != "" {
			query.Set("domainId", opts.DomainID)
		}

		body, err := c.do(ctx, request{
			method:   http.MethodGet,
			segments: []string{"dataSource"},
			query:    query,
		})
		if err != nil {
			return nil, err
		}

		var decoded struct {
			Hits []dataSourcePayload `json:"hits"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(body), &decoded); err != nil {
			return nil, fmt.Errorf("decoding data source search: %w", err)
		}
		for _, hit := range decoded.Hits {
			dataSource, err := hit.dataSource()
			if err != nil {
				return nil, err
			}
			dataSources = append(dataSources, *dataSource)
		}
		offset += len(decoded.Hits)

		if len(decoded.Hits) < dataSourceSearchPageSize {
			return dataSources, nil
		}
	}
}

// AddDomainDataSources adds data sources to a domain with manual assignment,
// with the documented POST /domain/{domainId}/datasources. Immuta adds them
// in a background job, whose id is returned.
func (c *Client) AddDomainDataSources(ctx context.Context, domainID string, dataSourceIDs []string) (string, error) {
	if err := validateDomainID(domainID); err != nil {
		return "", err
	}
	if len(dataSourceIDs) == 0 {
		return "", errors.New("at least one data source id is required")
	}

	body := make([]map[string]int64, 0, len(dataSourceIDs))
	for _, id := range dataSourceIDs {
		number, err := parseDataSourceID(id)
		if err != nil {
			return "", err
		}
		body = append(body, map[string]int64{"dataSourceId": number})
	}

	response, err := c.do(ctx, request{
		method:   http.MethodPost,
		segments: []string{"domain", domainID, "datasources"},
		body:     body,
	})
	if err != nil {
		return "", err
	}

	var decoded struct {
		JobID string `json:"jobId"`
	}
	if trimmed := bytes.TrimSpace(response); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &decoded); err != nil {
			return "", fmt.Errorf("decoding domain data source response: %w", err)
		}
	}

	return decoded.JobID, nil
}

// RemoveDomainDataSource removes a data source from a domain with manual
// assignment, with the documented
// DELETE /domain/{domainId}/datasources/{dataSourceId}.
func (c *Client) RemoveDomainDataSource(ctx context.Context, domainID, dataSourceID string) error {
	if err := validateDomainID(domainID); err != nil {
		return err
	}
	if _, err := parseDataSourceID(dataSourceID); err != nil {
		return err
	}

	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		segments: []string{"domain", domainID, "datasources", dataSourceID},
	})

	return err
}

// parseDataSourceID checks that a data source id is the documented integer.
func parseDataSourceID(id string) (int64, error) {
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil || number < 1 {
		return 0, fmt.Errorf("a data source id must be a positive integer, got %q", id)
	}

	return number, nil
}

// dataSourcePayload is the documented data source object.
type dataSourcePayload struct {
	ID                 json.RawMessage `json:"id"`
	Name               string          `json:"name"`
	ConnectionKey      string          `json:"connectionKey"`
	NamePath           []string        `json:"namePath"`
	DataObjectUniqueID string          `json:"dataObjectUniqueId"`
}

func (p dataSourcePayload) dataSource() (*DataSource, error) {
	// Documented as an integer; text is accepted too.
	id, err := flexibleID(p.ID)
	if err != nil || id == "" {
		return nil, errors.New("decoding data source: a data source has no id")
	}

	return &DataSource{
		ID:            id,
		Name:          p.Name,
		ConnectionKey: p.ConnectionKey,
		NamePath:      p.NamePath,
		DataObjectID:  p.DataObjectUniqueID,
	}, nil
}
