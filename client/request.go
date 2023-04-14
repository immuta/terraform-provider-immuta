package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (c *ImmutaClient) Head(path, version string, query map[string]string) error {
	return c.doRequest(http.MethodHead, path, version, query, nil, nil)
}

func (c *ImmutaClient) Get(path, version string, query map[string]string, output interface{}) error {
	return c.doRequest(http.MethodGet, path, version, query, nil, output)
}

func (c *ImmutaClient) Post(path, version string, params interface{}, output interface{}) error {
	return c.doRequest(http.MethodPost, path, version, nil, params, output)
}

func (c *ImmutaClient) PostWithQuery(path, version string, params interface{}, query map[string]string, output interface{}) error {
	return c.doRequest(http.MethodPost, path, version, query, params, output)
}

func (c *ImmutaClient) Put(path, version string, params interface{}, output interface{}) error {
	return c.doRequest(http.MethodPut, path, version, nil, params, output)
}

func (c *ImmutaClient) Patch(path, version string, params interface{}, output interface{}) error {
	return c.doRequest(http.MethodPatch, path, version, nil, params, output)
}

func (c *ImmutaClient) Delete(path, version string, params interface{}, output interface{}) error {
	return c.doRequest(http.MethodDelete, path, version, nil, params, output)
}

func (c *ImmutaClient) DeleteWithQuery(path, version string, params interface{}, query map[string]string, output interface{}) error {
	return c.doRequest(http.MethodDelete, path, version, query, params, output)
}

func (c *ImmutaClient) doRequest(method string, path string, version string, query map[string]string, params interface{}, output interface{}) error {

	var bodyReader io.Reader = nil

	if params != nil {
		body, err := json.Marshal(params)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(body)
	}

	url := c.makeUrl(path)
	request, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return err
	}

	for k, v := range c.DefaultHeaders {
		request.Header.Set(k, v)
	}

	if version != "" {
		request.Header.Set("Accept", fmt.Sprintf("application/json; version=#{version}"))
	}

	if query != nil {
		for k, v := range query {
			q := request.URL.Query()
			q.Add(k, v)

			request.URL.RawQuery = q.Encode()
		}
	}

	response, err := c.Client.Do(request)
	defer c.Client.CloseIdleConnections()

	if err != nil {
		return err
	}

	if response.StatusCode >= 400 {
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(response.Body)

		if err != nil {
			return err
		}

		newStr := buf.String()
		var err error
		if response.StatusCode == 404 {
			err = NewNotFoundError(newStr)
		} else {
			err = NewRequestError(response.StatusCode, newStr)
		}

		return err
	}

	return c.unmarshall(response.Body, output)
}
