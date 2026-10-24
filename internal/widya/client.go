// Package widya reads the production figures off Widya Load Scanner, the
// weighbridge system the site's tickets are cut in. Nothing is copied into the
// spreadsheets yet: the figures are read when somebody looks, and shown.
package widya

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stats is the scanner's own dashboard: how many of each thing it holds.
type Stats struct {
	Tickets   int `json:"ticketsCount"`
	Vehicles  int `json:"vehiclesCount"`
	Materials int `json:"materialsCount"`
	Suppliers int `json:"suppliersCount"`
	Operators int `json:"operatorsCount"`
	Units     int `json:"unitsCount"`
	Users     int `json:"usersCount"`
	Webhooks  int `json:"webhooksCount"`
}

// MaterialVolume is how much of one material has crossed the scanner.
type MaterialVolume struct {
	Name   string
	Volume float64
}

// Snapshot is one reading of the scanner, and when it was taken.
type Snapshot struct {
	Stats     Stats
	Materials []MaterialVolume
	FetchedAt time.Time
}

// errUnauthorized is a token the scanner no longer accepts. It is answered by
// signing in again, once.
var errUnauthorized = errors.New("widya: unauthorized")

// Client signs in to the scanner and reads its dashboard. The token and the
// company it answers for are kept between calls and renewed when refused.
type Client struct {
	baseURL      string
	email        string
	password     string
	loginCompany string
	http         *http.Client

	mu        sync.Mutex
	token     string
	companyID string
}

// NewClient points a client at a scanner. loginCompany is the Company-Id the
// sign-in form sends. Left empty, it is read off the scanner's own public
// config, which is where its sign-in page reads it from.
func NewClient(baseURL, email, password, loginCompany string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		baseURL:      siteRoot(baseURL),
		email:        strings.TrimSpace(email),
		password:     password,
		loginCompany: strings.TrimSpace(loginCompany),
		http:         httpClient,
	}
}

// Fetch reads the dashboard figures and the volume per material.
func (c *Client) Fetch(ctx context.Context) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot, err := c.fetch(ctx)
	if errors.Is(err, errUnauthorized) {
		// The token has expired, or was never there. Sign in and try once more.
		c.token, c.companyID = "", ""
		snapshot, err = c.fetch(ctx)
	}
	return snapshot, err
}

func (c *Client) fetch(ctx context.Context) (Snapshot, error) {
	if c.token == "" {
		if err := c.signIn(ctx); err != nil {
			return Snapshot{}, err
		}
	}
	var stats Stats
	if err := c.get(ctx, "/api/v1/dashboard/statistics", &stats); err != nil {
		return Snapshot{}, fmt.Errorf("read statistics: %w", err)
	}
	var volumes []struct {
		Name   string          `json:"name"`
		Volume json.RawMessage `json:"volume"`
	}
	if err := c.get(ctx, "/api/v1/dashboard/volume-per-materials", &volumes); err != nil {
		return Snapshot{}, fmt.Errorf("read volume per material: %w", err)
	}
	snapshot := Snapshot{Stats: stats, FetchedAt: time.Now()}
	for _, line := range volumes {
		snapshot.Materials = append(snapshot.Materials, MaterialVolume{
			Name:   strings.TrimSpace(line.Name),
			Volume: parseVolume(line.Volume),
		})
	}
	return snapshot, nil
}

// signIn trades the account for a token, then asks who that token is, because
// the dashboard is read per company and the company is the account's.
func (c *Client) signIn(ctx context.Context) error {
	if c.loginCompany == "" {
		company, err := c.discoverLoginCompany(ctx)
		if err != nil {
			return fmt.Errorf("read widya config: %w", err)
		}
		c.loginCompany = company
	}
	body, err := json.Marshal(map[string]string{"email": c.email, "password": c.password})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Company-Id", c.loginCompany)
	var login struct {
		AccessToken string `json:"accessToken"`
	}
	if err := c.do(request, &login); err != nil {
		if errors.Is(err, errUnauthorized) {
			return fmt.Errorf("login widya ditolak: periksa email dan password")
		}
		return fmt.Errorf("login widya: %w", err)
	}
	if strings.TrimSpace(login.AccessToken) == "" {
		return fmt.Errorf("login widya: tidak ada token pada jawaban")
	}
	c.token = strings.TrimSpace(login.AccessToken)

	var user struct {
		CompanyID string `json:"companyId"`
	}
	if err := c.get(ctx, "/api/v1/auth/user", &user); err != nil {
		c.token = ""
		return fmt.Errorf("read widya user: %w", err)
	}
	c.companyID = strings.TrimSpace(user.CompanyID)
	return nil
}

// discoverLoginCompany reads the Company-Id the scanner's sign-in page sends.
// The page falls back to "robotics" when the config names none, and so does
// this.
func (c *Client) discoverLoginCompany(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/config/ui", nil)
	if err != nil {
		return "", err
	}
	var config struct {
		Features struct {
			LoginCompanyID string `json:"loginCompanyId"`
		} `json:"features"`
	}
	if err := c.do(request, &config); err != nil {
		return "", err
	}
	if company := strings.TrimSpace(config.Features.LoginCompanyID); company != "" {
		return company, nil
	}
	return "robotics", nil
}

func (c *Client) get(ctx context.Context, path string, into interface{}) error {
	url := c.baseURL + path
	if c.companyID != "" {
		url += "?companyId=" + c.companyID
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	return c.do(request, into)
}

func (c *Client) do(request *http.Request, into interface{}) error {
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return errUnauthorized
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// parseVolume reads a volume the scanner sends as a quoted decimal
// ("342008.7793...") and would be within its rights to send as a number.
func parseVolume(raw json.RawMessage) float64 {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return value
}

// siteRoot keeps only the scheme and host of the address it is given. The API
// hangs off the site root, and the address people copy is whatever page they
// were on: "https://wls-star.widyarobotics.com/login" has to work as well.
func siteRoot(address string) string {
	address = strings.TrimSpace(address)
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(address, "/")
	}
	return parsed.Scheme + "://" + parsed.Host
}
