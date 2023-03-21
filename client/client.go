package client

import (
	"fmt"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/logging"
	"net/http"
	"time"
)

// ImmutaClient is used to make requests to the Immuta API
type ImmutaClient struct {
	Host           string
	DefaultHeaders map[string]string
	Client         http.Client
	Timeout        int
}

func NewClient(host, apiToken, userAgent string) *ImmutaClient {
	httpClient := cleanhttp.DefaultClient()
	httpClient.Transport = logging.NewSubsystemLoggingHTTPTransport("Immuta", httpClient.Transport)
	httpClient.Timeout = 60 * time.Second

	client := &ImmutaClient{
		Host: host,
		DefaultHeaders: map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   userAgent,
		},
		Client: *httpClient,
	}

	client.DefaultHeaders["Authorization"] = fmt.Sprintf("Bearer %s", apiToken)

	return client
}

func (c *ImmutaClient) makeUrl(path string) string {
	return fmt.Sprintf("https://%s%s", c.Host, path)
}

//func (a *ImmutaClient) Validate(ctx context.Context) error {
//	purposeApi := a.NewPurposeAPI()
//	purposes, err := purposeApi.ListPurposes()
//	if purposes.Count < 1 {
//		return errors.New("no purposes found")
//	}
//	return err
//}
