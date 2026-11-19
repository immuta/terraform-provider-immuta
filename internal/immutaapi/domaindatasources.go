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
)

// domainDataSourcePageSize is the page size used to list every data source
// in a domain.
const domainDataSourcePageSize = 100

// DomainDataSource is a data source in a domain, as returned by the
// documented GET /domain/{domainId}/datasources.
type DomainDataSource struct {
	ID       string
	Name     string
	Platform string
}

// ListDomainDataSources returns every data source in a domain, with the
// documented GET /domain/{domainId}/datasources, following its pages.
// searchText, when set, keeps only data sources whose name contains it.
func (c *Client) ListDomainDataSources(ctx context.Context, domainID, searchText string) ([]DomainDataSource, error) {
	if err := validateDomainID(domainID); err != nil {
		return nil, err
	}

	var dataSources []DomainDataSource
	for offset := 0; ; {
		query := url.Values{}
		query.Set("offset", strconv.Itoa(offset))
		query.Set("size", strconv.Itoa(domainDataSourcePageSize))
		if searchText != "" {
			query.Set("searchText", searchText)
		}

		body, err := c.do(ctx, request{
			method:   http.MethodGet,
			segments: []string{"domain", domainID, "datasources"},
			query:    query,
		})
		if err != nil {
			return nil, err
		}

		page, total, err := decodeDomainDataSources(body)
		if err != nil {
			return nil, err
		}
		dataSources = append(dataSources, page...)
		offset += len(page)

		if len(page) < domainDataSourcePageSize || (total != nil && offset >= *total) {
			return dataSources, nil
		}
	}
}

func decodeDomainDataSources(body []byte) ([]DomainDataSource, *int, error) {
	var decoded struct {
		Data []struct {
			DataSourceID json.RawMessage `json:"dataSourceId"`
			Name         string          `json:"name"`
			Platform     string          `json:"platform"`
		} `json:"data"`
		Total *int `json:"total"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &decoded); err != nil {
		return nil, nil, fmt.Errorf("decoding domain data sources: %w", err)
	}

	dataSources := make([]DomainDataSource, 0, len(decoded.Data))
	for _, item := range decoded.Data {
		// Documented as an integer; text is accepted too.
		id, err := flexibleID(item.DataSourceID)
		if err != nil || id == "" {
			return nil, nil, errors.New("decoding domain data sources: a data source has no dataSourceId")
		}
		dataSources = append(dataSources, DomainDataSource{ID: id, Name: item.Name, Platform: item.Platform})
	}

	return dataSources, decoded.Total, nil
}
