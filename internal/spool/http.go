package spool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
)

type HTTPRequest struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Header map[string][]string `json:"header,omitempty"`
	Body   []byte              `json:"body"`
}

func (h *HTTPRequest) Marshal() ([]byte, error) {
	if h == nil {
		return nil, fmt.Errorf("spool: nil http request")
	}
	return json.Marshal(h)
}

func (h *HTTPRequest) Unmarshal(data []byte) error {
	if h == nil {
		return fmt.Errorf("spool: nil http request")
	}
	return json.Unmarshal(data, h)
}

func HTTPHandler(client *http.Client) Handler {
	return func(ctx context.Context, payload []byte) (err error) {
		req, err := unmarshalAndValidateRequest(payload)
		if err != nil {
			return err
		}

		httpReq, err := buildHTTPRequest(ctx, req)
		if err != nil {
			return err
		}

		resp, err := executeHTTPRequest(client, httpReq)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := resp.Body.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
		}()

		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			return copyErr
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("spool: remote status %d", resp.StatusCode)
		}
		return nil
	}
}

func unmarshalAndValidateRequest(payload []byte) (*HTTPRequest, error) {
	var req HTTPRequest
	if err := req.Unmarshal(payload); err != nil {
		return nil, ErrCorrupt
	}
	if !regexp.MustCompile(`^(http|https)://`).MatchString(req.URL) {
		return nil, ErrCorrupt
	}
	parsedURL, err := url.ParseRequestURI(req.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return nil, ErrCorrupt
	}
	return &req, nil
}

func buildHTTPRequest(ctx context.Context, req *HTTPRequest) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	for key, values := range req.Header {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}
	return httpReq, nil
}

func executeHTTPRequest(client *http.Client, httpReq *http.Request) (*http.Response, error) {
	if client.Transport != nil {
		return client.Transport.RoundTrip(httpReq)
	}
	return http.DefaultTransport.RoundTrip(httpReq)
}
