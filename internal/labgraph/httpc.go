package labgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is the host CLI HTTP client of a running labgraph service.
type Client struct {
	Base   string
	Token  string
	HTTP   *http.Client
}

func NewClient(base, tokenFile string) (*Client, error) {
	tok, err := readTokenFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("labgraph token: %w", err)
	}
	return &Client{
		Base:  strings.TrimRight(base, "/"),
		Token: tok,
		HTTP:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *Client) authHeader() string { return "Bearer " + c.Token }

func (c *Client) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("labgraph %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) Validate(name string) (*GraphResult, error) {
	var res GraphResult
	err := c.do(http.MethodPost, "/v1/scenarios/"+name+":validate", map[string]any{}, &res)
	return &res, err
}

func (c *Client) Plan(name string) (*GraphResult, error) {
	var res GraphResult
	err := c.do(http.MethodPost, "/v1/scenarios/"+name+":plan", map[string]any{}, &res)
	return &res, err
}

func (c *Client) Apply(name string) (*GraphResult, error) {
	var res GraphResult
	err := c.do(http.MethodPost, "/v1/scenarios/"+name+":apply", map[string]any{}, &res)
	return &res, err
}

func (c *Client) Reset(name string, appliances []string) (*GraphResult, error) {
	var res GraphResult
	err := c.do(http.MethodPost, "/v1/scenarios/"+name+":reset", ResetRequest{Appliances: appliances}, &res)
	return &res, err
}
