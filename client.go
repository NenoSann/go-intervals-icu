package intervalsicu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
)

const defaultBaseURL = "https://intervals.icu"

// Client is a minimal Intervals.icu API client using API key auth.
type Client struct {
	baseURL   *url.URL
	apiKey    string
	athleteID string
	http      *http.Client
}

// Option configures the client.
type Option func(*Client) error

// WithBaseURL overrides the default API base URL.
func WithBaseURL(raw string) Option {
	return func(c *Client) error {
		parsed, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("parse base url: %w", err)
		}
		c.baseURL = parsed
		return nil
	}
}

// WithHTTPClient overrides the default http client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) error {
		if client == nil {
			return fmt.Errorf("http client is nil")
		}
		c.http = client
		return nil
	}
}

// NewClient initializes a client with API key auth and the default athlete id.
func NewClient(apiKey, athleteID string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("apiKey is required")
	}
	if strings.TrimSpace(athleteID) == "" {
		return nil, fmt.Errorf("athleteID is required")
	}
	base, err := url.Parse(defaultBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse default base url: %w", err)
	}
	c := &Client{
		baseURL:   base,
		apiKey:    apiKey,
		athleteID: athleteID,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: 30 * time.Second}
	}
	return c, nil
}

// do executes an HTTP request and decodes the response into out if provided.
func (c *Client) do(ctx context.Context, method, path string, pathParams map[string]string, query url.Values, body any, out any) error {
	if c == nil {
		return fmt.Errorf("client is nil")
	}
	resolvedPath := path
	for key, value := range pathParams {
		resolvedPath = strings.ReplaceAll(resolvedPath, "{"+key+"}", url.PathEscape(value))
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: resolvedPath})
	if query != nil && len(query) > 0 {
		endpoint.RawQuery = query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth("API_KEY", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return &Error{StatusCode: resp.StatusCode, Body: string(payload)}
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// doMultipart executes an HTTP request using multipart/form-data body.
func (c *Client) doMultipart(ctx context.Context, method, path string, pathParams map[string]string, query url.Values, body any, out any) error {
	if c == nil {
		return fmt.Errorf("client is nil")
	}
	resolvedPath := path
	for key, value := range pathParams {
		resolvedPath = strings.ReplaceAll(resolvedPath, "{"+key+"}", url.PathEscape(value))
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: resolvedPath})
	if query != nil && len(query) > 0 {
		endpoint.RawQuery = query.Encode()
	}

	multipartBody := &bytes.Buffer{}
	writer := multipart.NewWriter(multipartBody)
	if body != nil {
		if err := writeMultipartBody(writer, body); err != nil {
			return fmt.Errorf("build multipart body: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), multipartBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth("API_KEY", c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return &Error{StatusCode: resp.StatusCode, Body: string(payload)}
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func writeMultipartBody(writer *multipart.Writer, body any) error {
	if writer == nil {
		return fmt.Errorf("multipart writer is nil")
	}
	if body == nil {
		return nil
	}

	v := reflect.ValueOf(body)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("multipart body must be struct or struct pointer")
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" { // unexported
			continue
		}

		key := sf.Name
		if tag := sf.Tag.Get("json"); tag != "" {
			tagName := strings.Split(tag, ",")[0]
			if tagName == "-" {
				continue
			}
			if tagName != "" {
				key = tagName
			}
		}

		fv := v.Field(i)
		if !fv.IsValid() {
			continue
		}
		for fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				goto nextField
			}
			fv = fv.Elem()
		}

		switch fv.Kind() {
		case reflect.Slice:
			if fv.Type().Elem().Kind() == reflect.Uint8 {
				fileWriter, err := writer.CreateFormFile(key, key+".bin")
				if err != nil {
					return err
				}
				if _, err := fileWriter.Write(fv.Bytes()); err != nil {
					return err
				}
				goto nextField
			}
			if err := writer.WriteField(key, fmt.Sprint(fv.Interface())); err != nil {
				return err
			}
		default:
			if err := writer.WriteField(key, fmt.Sprint(fv.Interface())); err != nil {
				return err
			}
		}

	nextField:
	}

	return nil
}

func (c *Client) athleteIDOr(value string) string {
	if strings.TrimSpace(value) == "" {
		return c.athleteID
	}
	return value
}

// Error represents a non-2xx API response.
type Error struct {
	StatusCode int
	Body       string
}

func (e *Error) Error() string {
	if e == nil {
		return "intervalsicu: unknown error"
	}
	if e.Body == "" {
		return fmt.Sprintf("intervalsicu: status %d", e.StatusCode)
	}
	return fmt.Sprintf("intervalsicu: status %d: %s", e.StatusCode, e.Body)
}
