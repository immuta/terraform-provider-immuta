package immutaapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const dataSourceObject = `{
	"id": 22,
	"name": "SnowflakeConnection . snowflake . public . tcp",
	"dataObjectUniqueId": "4eed11f5-1368-4b75-afda-e7c5634544b4",
	"namePath": ["snowflake", "public", "tcp"],
	"connectionKey": "SnowflakeConnection",
	"sqlTableName": "tcp"
}`

var wantDataSource = DataSource{
	ID:            "22",
	Name:          "SnowflakeConnection . snowflake . public . tcp",
	ConnectionKey: "SnowflakeConnection",
	NamePath:      []string{"snowflake", "public", "tcp"},
	DataObjectID:  "4eed11f5-1368-4b75-afda-e7c5634544b4",
}

func TestGetDataSourceByName(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, dataSourceObject)

	dataSource, err := newTestClient(t, server.URL).GetDataSourceByName(t.Context(), "SnowflakeConnection . snowflake . public . tcp")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !reflect.DeepEqual(*dataSource, wantDataSource) {
		t.Errorf("data source = %+v, want %+v", *dataSource, wantDataSource)
	}
	if got := (*requests)[0]; got.method != http.MethodGet || got.path != "/dataSource/name/SnowflakeConnection%20.%20snowflake%20.%20public%20.%20tcp" {
		t.Errorf("request = %s %s", got.method, got.path)
	}
}

func TestSearchDataSourcesFollowsPages(t *testing.T) {
	t.Parallel()

	const total = dataSourceSearchPageSize + 1

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/dataSource" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		queries = append(queries, r.URL.RawQuery)

		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		var hits []string
		for i := offset; i < min(offset+size, total); i++ {
			hits = append(hits, fmt.Sprintf(`{"id": %d, "name": "ds %d"}`, i+1, i+1))
		}
		writeTestJSON(w, http.StatusOK, fmt.Sprintf(`{"count": %d, "hits": [%s]}`, total, strings.Join(hits, ",")))
	}))
	t.Cleanup(server.Close)

	dataSources, err := newTestClient(t, server.URL).SearchDataSources(t.Context(), SearchDataSourcesOptions{SearchText: "tcp"})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(dataSources) != total || dataSources[total-1].ID != strconv.Itoa(total) {
		t.Fatalf("got %d data sources, last %+v", len(dataSources), dataSources[len(dataSources)-1])
	}
	want := []string{
		"nameOnly=true&offset=0&searchText=tcp&size=100",
		"nameOnly=true&offset=100&searchText=tcp&size=100",
	}
	if !reflect.DeepEqual(queries, want) {
		t.Errorf("queries = %v, want %v", queries, want)
	}
}

func TestSearchDataSourcesInDomain(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeTestJSON(w, http.StatusOK, `{"count": 1, "hits": [`+dataSourceObject+`]}`)
	}))
	t.Cleanup(server.Close)

	dataSources, err := newTestClient(t, server.URL).SearchDataSources(t.Context(), SearchDataSourcesOptions{DataSourceIDs: []string{"22"}, DomainID: "d1"})
	if err != nil || len(dataSources) != 1 || !reflect.DeepEqual(dataSources[0], wantDataSource) {
		t.Fatalf("data sources = %+v, err = %v", dataSources, err)
	}
	// No searchText, so no nameOnly either.
	if query != "dataSourceIds=22&domainId=d1&offset=0&size=100" {
		t.Errorf("query = %s", query)
	}
}

func TestDomainDataSourceMembership(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusOK, `{"jobId": "e48e15a0-ef5c-11ed-892c-a75afcd86969"}`)
	client := newTestClient(t, server.URL)

	jobID, err := client.AddDomainDataSources(t.Context(), "d1", []string{"36", "37"})
	if err != nil || jobID != "e48e15a0-ef5c-11ed-892c-a75afcd86969" {
		t.Fatalf("job = %q, err = %v", jobID, err)
	}
	if err := client.RemoveDomainDataSource(t.Context(), "d1", "36"); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := []recorded{
		// Documented as integers.
		{http.MethodPost, "/domain/d1/datasources", testAPIKey, []any{map[string]any{"dataSourceId": float64(36)}, map[string]any{"dataSourceId": float64(37)}}},
		{http.MethodDelete, "/domain/d1/datasources/36", testAPIKey, nil},
	}
	if !reflect.DeepEqual(*requests, want) {
		t.Errorf("requests = %+v, want %+v", *requests, want)
	}
}

func TestDataSourceErrors(t *testing.T) {
	t.Parallel()

	server, requests := recordingServer(t, http.StatusNotFound, `{"message": "Data source not found"}`)
	client := newTestClient(t, server.URL)

	if _, err := client.GetDataSourceByName(t.Context(), "missing"); !IsNotFound(err) {
		t.Errorf("error = %v, want not found", err)
	}

	before := len(*requests)
	for name, err := range map[string]error{
		"no name":           func() error { _, err := client.GetDataSourceByName(t.Context(), " "); return err }(),
		"no filter":         func() error { _, err := client.SearchDataSources(t.Context(), SearchDataSourcesOptions{}); return err }(),
		"no data sources":   func() error { _, err := client.AddDomainDataSources(t.Context(), "d1", nil); return err }(),
		"non-numeric id":    func() error { _, err := client.AddDomainDataSources(t.Context(), "d1", []string{"orders"}); return err }(),
		"zero id":           client.RemoveDomainDataSource(t.Context(), "d1", "0"),
		"no domain":         client.RemoveDomainDataSource(t.Context(), "", "36"),
		"no domain for add": func() error { _, err := client.AddDomainDataSources(t.Context(), " ", []string{"36"}); return err }(),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(*requests) != before {
		t.Error("invalid arguments reached the API")
	}
}
