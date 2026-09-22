package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type DiagnosticClient struct{ client *http.Client }

func NewDiagnosticClient(timeout time.Duration) *DiagnosticClient {
	return &DiagnosticClient{client: &http.Client{Timeout: timeout}}
}

func (c *DiagnosticClient) Diag(endpoint string) (map[string]any, error) {
	var result map[string]any
	err := c.get(endpoint, "/v1/diag", &result)
	return result, err
}

func (c *DiagnosticClient) Detail(endpoint string) (map[string]map[string]any, error) {
	var result map[string]map[string]any
	if err := c.get(endpoint, "/v1/detail", &result); err != nil {
		return nil, err
	}
	if len(result) != 2 || result["info"] == nil || result["diag"] == nil {
		return nil, fmt.Errorf("detail响应不符合协议")
	}
	return result, nil
}

func (c *DiagnosticClient) get(endpoint, path string, target any) error {
	endpoint = strings.TrimSpace(endpoint)
	if _, _, err := net.SplitHostPort(endpoint); err != nil {
		return fmt.Errorf("无效地址 %q", endpoint)
	}
	resp, err := c.client.Get("http://" + endpoint + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("解析响应: %w", err)
	}
	return nil
}
