package client

import (
	"encoding/json"
	"io"
	"io/ioutil"
)

func (c *ImmutaClient) unmarshall(reader io.ReadCloser, output interface{}) error {
	defer func() {
		_ = reader.Close()
	}()

	if output == nil {
		return nil
	}

	content, _ := ioutil.ReadAll(reader)

	if len(content) > 0 {
		err := json.Unmarshal(content, output)
		if err != nil {
			return err
		}
	}

	return nil
}
