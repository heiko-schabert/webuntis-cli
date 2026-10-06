package untis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client talks to the internal JSON-RPC endpoint the Untis Mobile app uses.
// Each request carries its own TOTP, so there is no session to keep.
type Client struct {
	cfg  Config
	http *http.Client

	mu       sync.Mutex
	user     *userData
	master   *masterData
	masterTS int64
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}}
}

// endpoint accepts a bare host as shown in WebUntis, or a URL for tests.
func (c *Client) endpoint() string {
	base := c.cfg.Server
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	return strings.TrimRight(base, "/") + "/WebUntis/jsonrpc_intern.do?school=" + url.QueryEscape(c.cfg.School)
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) rpc(ctx context.Context, method string, params map[string]any, out any) error {
	otp, err := totp(c.cfg.Secret, time.Now())
	if err != nil {
		return err
	}
	params["auth"] = map[string]any{"user": c.cfg.User, "otp": otp, "clientTime": time.Now().UnixMilli()}
	body, err := json.Marshal(map[string]any{"id": method, "method": method, "params": []any{params}, "jsonrpc": "2.0"})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "UntisMobileAndroid")
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connection to %s failed: %w", c.cfg.Server, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	slog.DebugContext(ctx, "rpc", "method", method, "status", resp.StatusCode, "bytes", len(b), "duration", time.Since(start).Round(time.Millisecond))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", method, resp.Status)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if r.Error != nil {
		return fmt.Errorf("WebUntis API error (%d): %s", r.Error.Code, r.Error.Message)
	}
	if out == nil || len(r.Result) == 0 {
		return nil
	}
	return json.Unmarshal(r.Result, out)
}

type userData struct {
	ElemType    string `json:"elemType"`
	ElemID      int    `json:"elemId"`
	DisplayName string `json:"displayName"`
	SchoolName  string `json:"schoolName"`
	Children    []struct {
		ID        int    `json:"id"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"children"`
	kids []Child
}

// Child is a student the account may see.
type Child struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
}

func (c *Client) CheckLogin(ctx context.Context) error {
	c.mu.Lock()
	c.user = nil
	c.mu.Unlock()
	_, err := c.userData(ctx)
	return err
}

func (c *Client) userData(ctx context.Context) (*userData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.user != nil {
		return c.user, nil
	}
	var r struct {
		UserData userData `json:"userData"`
	}
	if err := c.rpc(ctx, "getUserData2017", map[string]any{"deviceOs": "AND", "deviceOsVersion": "14"}, &r); err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}
	u := r.UserData
	for _, k := range u.Children {
		u.kids = append(u.kids, Child{k.ID, k.FirstName, k.LastName})
	}
	// Student accounts have no children; the student is their own subject.
	if len(u.kids) == 0 && strings.EqualFold(u.ElemType, "STUDENT") && u.ElemID != 0 {
		u.kids = []Child{{ID: u.ElemID, FirstName: u.DisplayName}}
	}
	if len(u.kids) == 0 {
		return nil, errors.New("no children linked to this account")
	}
	slog.InfoContext(ctx, "logged in", "school", u.SchoolName, "children", len(u.kids))
	c.user = &u
	return c.user, nil
}

// Children lists the students on the account.
func (c *Client) Children(ctx context.Context) ([]Child, error) {
	u, err := c.userData(ctx)
	if err != nil {
		return nil, err
	}
	return u.kids, nil
}

// child resolves a first name, falling back to WEBUNTIS_STUDENT, then the only child.
func (c *Client) child(ctx context.Context, name string) (Child, error) {
	kids, err := c.Children(ctx)
	if err != nil {
		return Child{}, err
	}
	if name == "" {
		name = c.cfg.Student
	}
	if name == "" || len(kids) == 1 {
		return kids[0], nil
	}
	var names []string
	for _, k := range kids {
		if strings.Contains(strings.ToLower(k.FirstName), strings.ToLower(strings.TrimSpace(name))) {
			return k, nil
		}
		names = append(names, k.FirstName)
	}
	return Child{}, fmt.Errorf("child %q not found, available: %s", name, strings.Join(names, ", "))
}
