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

func TestListDomainDataSourcesFollowsPages(t *testing.T) {
	t.Parallel()

	const total = domainDataSourcePageSize + 2

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/domain/d%2F1/datasources" {
			t.Errorf("request = %s %s, want GET /domain/d%%2F1/datasources", r.Method, r.URL.EscapedPath())
		}
		queries = append(queries, r.URL.RawQuery)

		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		var items []string
		for i := offset; i < min(offset+size, total); i++ {
			// Documented as integers.
			items = append(items, fmt.Sprintf(`{"dataSourceId": %d, "name": "ds %d", "platform": "Snowflake", "tags": []}`, i+1, i+1))
		}
		writeTestJSON(w, http.StatusOK, fmt.Sprintf(`{"data": [%s], "total": %d}`, strings.Join(items, ","), total))
	}))
	t.Cleanup(server.Close)

	dataSources, err := newTestClient(t, server.URL).ListDomainDataSources(t.Context(), "d/1", "sales data")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if len(dataSources) != total {
		t.Fatalf("got %d data sources, want %d", len(dataSources), total)
	}
	if got, want := dataSources[total-1], (DomainDataSource{ID: strconv.Itoa(total), Name: fmt.Sprintf("ds %d", total), Platform: "Snowflake"}); got != want {
		t.Errorf("last data source = %+v, want %+v", got, want)
	}
	wantQueries := []string{
		"offset=0&searchText=sales+data&size=100",
		"offset=100&searchText=sales+data&size=100",
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Errorf("queries = %v, want %v", queries, wantQueries)
	}
}

func TestListDomainDataSourcesErrors(t *testing.T) {
	t.Parallel()

	server, _ := recordingServer(t, http.StatusNotFound, `{"message": "Domain not found"}`)
	client := newTestClient(t, server.URL)

	if _, err := client.ListDomainDataSources(t.Context(), "missing", ""); !IsNotFound(err) {
		t.Errorf("error = %v, want not found", err)
	}
	if _, err := client.ListDomainDataSources(t.Context(), " ", ""); err == nil {
		t.Error("ListDomainDataSources accepted an empty domain id")
	}
	if _, _, err := decodeDomainDataSources([]byte(`{"data": [{"name": "no id"}]}`)); err == nil {
		t.Error("decodeDomainDataSources accepted a data source without an id")
	}
}
