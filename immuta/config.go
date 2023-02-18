package immuta

import (
	"fmt"
	"github.com/instacart/terraform-provider-immuta/client"
)

type Config struct {
	APIKey string
	Host   string
}

func (config *Config) ImmutaClient() (interface{}, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("no Host set")
	}
	if config.APIKey == "" {
		return nil, fmt.Errorf("no API Key set")
	}

	userAgent := fmt.Sprintf("HashiCorp Terraform/%s (+https://www.terraform.io) Terraform Plugin SDK/%s", "immuta", "immuta")

	client := client.NewClient(config.Host, config.APIKey, userAgent)

	// todo validate client once low cost API call is available
	err := error(nil)

	return client, err
}
