package immuta

import (
	"fmt"
	"github.com/immuta/terraform-provider-immuta/client"
)

type Config struct {
	APIToken string
	Host     string
}

func (config *Config) ImmutaClient() (interface{}, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("no Host set")
	}
	if config.APIToken == "" {
		return nil, fmt.Errorf("no API Key set")
	}

	userAgent := fmt.Sprintf("HashiCorp Terraform/%s (+https://www.terraform.io) Terraform Plugin SDK/%s", "immuta", "immuta")

	immutaClient := client.NewClient(config.Host, config.APIToken, userAgent)

	// todo validate client once low cost API call is available
	err := error(nil)

	return immutaClient, err
}
