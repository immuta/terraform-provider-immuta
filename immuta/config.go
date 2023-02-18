package immuta

type Config struct {
	APIKey string
	Host   string
}

func (config *Config) Client() (*ImmutaClient, error) {
	//if err := config.validate(); err != nil {
	//	return nil, err
	//}

	return &ImmutaClient{
		APIKey: config.APIKey,
		Host:   config.Host,
	}, nil
}
