package gce

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	defaultBaseURL = "https://compute.googleapis.com"
	computeScope   = "https://www.googleapis.com/auth/cloud-platform"
)

// Client is a client for the GCE Alpha REST API.
type Client struct {
	httpClient *http.Client
	baseURL    string
	tokenSrc   oauth2.TokenSource
}

// ClientOption provides functional options for configuring Client.
type ClientOption func(*Client)

// WithBaseURL overrides the base URL for the GCE API (useful for testing).
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(url, "/")
	}
}

// WithHTTPClient overrides the underlying HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithTokenSource overrides the OAuth2 TokenSource.
func WithTokenSource(ts oauth2.TokenSource) ClientOption {
	return func(c *Client) {
		c.tokenSrc = ts
	}
}

// NewClient creates a new GCE Alpha API client.
func NewClient(ctx context.Context, opts ...ClientOption) (*Client, error) {
	c := &Client{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	// If no custom token source or custom HTTP client is set, find default credentials
	if c.tokenSrc == nil {
		creds, err := google.FindDefaultCredentials(ctx, computeScope)
		if err == nil {
			c.tokenSrc = creds.TokenSource
		}
		// Note: if creds are not found (e.g. running mock unit tests), tokenSrc remains nil.
	}

	return c, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values) (*http.Request, error) {
	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)
	if len(query) > 0 {
		reqURL = fmt.Sprintf("%s?%s", reqURL, query.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	if c.tokenSrc != nil {
		token, err := c.tokenSrc.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to get oauth2 token: %w", err)
		}
		token.SetAuthHeader(req)
	}

	return req, nil
}

func (c *Client) do(req *http.Request, target interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GCE API error: status=%d body=%s", resp.StatusCode, string(body))
	}

	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			return fmt.Errorf("failed to unmarshal JSON response: %w", err)
		}
	}

	return nil
}

// GetInstance fetches a single instance by project, zone and instance name.
func (c *Client) GetInstance(ctx context.Context, project, zone, instanceName string) (*Instance, error) {
	zone = cleanZoneName(zone)
	path := fmt.Sprintf("/compute/alpha/projects/%s/zones/%s/instances/%s", project, zone, instanceName)
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var inst Instance
	if err := c.do(req, &inst); err != nil {
		return nil, err
	}

	return &inst, nil
}

// ListInstances lists all instances in a given project and zone.
func (c *Client) ListInstances(ctx context.Context, project, zone string) ([]*Instance, error) {
	zone = cleanZoneName(zone)
	var allInstances []*Instance
	pageToken := ""

	for {
		path := fmt.Sprintf("/compute/alpha/projects/%s/zones/%s/instances", project, zone)
		query := url.Values{}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		req, err := c.newRequest(ctx, http.MethodGet, path, query)
		if err != nil {
			return nil, err
		}

		var resp InstanceList
		if err := c.do(req, &resp); err != nil {
			return nil, err
		}

		allInstances = append(allInstances, resp.Items...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return allInstances, nil
}

// AggregatedListInstances lists all instances across all zones in the given project.
func (c *Client) AggregatedListInstances(ctx context.Context, project string) ([]*Instance, error) {
	var allInstances []*Instance
	pageToken := ""

	for {
		path := fmt.Sprintf("/compute/alpha/projects/%s/aggregated/instances", project)
		query := url.Values{}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		req, err := c.newRequest(ctx, http.MethodGet, path, query)
		if err != nil {
			return nil, err
		}

		var resp InstanceAggregatedList
		if err := c.do(req, &resp); err != nil {
			return nil, err
		}

		for _, scopedList := range resp.Items {
			if len(scopedList.Instances) > 0 {
				allInstances = append(allInstances, scopedList.Instances...)
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return allInstances, nil
}

// ExtractHashedHostID checks if the instance has HashedHostID enabled and returns the ID.
// Returns (hashedHostID, isEnabled).
func ExtractHashedHostID(inst *Instance) (string, bool) {
	if inst == nil {
		return "", false
	}

	// Check exposeHostTopology flag or physicalHostTopology host
	if inst.ResourceStatus != nil && inst.ResourceStatus.PhysicalHostTopology != nil {
		hostID := inst.ResourceStatus.PhysicalHostTopology.Host
		if hostID != "" {
			return hostID, true
		}
	}

	if inst.ExposeHostTopology {
		// Enabled but physicalHostTopology might not have returned or host is empty
		return "", true
	}

	return "", false
}

// cleanZoneName extracts the short zone name from a full URL or returns the short name.
// e.g. "https://www.googleapis.com/compute/alpha/projects/p/zones/us-east1-d" -> "us-east1-d"
func cleanZoneName(zone string) string {
	if strings.Contains(zone, "/") {
		parts := strings.Split(zone, "/")
		return parts[len(parts)-1]
	}
	return zone
}
