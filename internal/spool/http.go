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
		var req HTTPRequest
		if err := req.Unmarshal(payload); err != nil {
			return ErrCorrupt
		}
		if !regexp.MustCompile(`^(http|https)://`).MatchString(req.URL) {
			return ErrCorrupt
		}
		parsedURL, err := url.ParseRequestURI(req.URL)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return ErrCorrupt
		}
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, parsedURL.String(), bytes.NewReader(req.Body))
		if err != nil {
			return err
		}
		for key, values := range req.Header {
			for _, value := range values {
				httpReq.Header.Add(key, value)
			}
		}
		var resp *http.Response
		if client.Transport != nil {
			resp, err = client.Transport.RoundTrip(httpReq)
		} else {
			resp, err = http.DefaultTransport.RoundTrip(httpReq)
		}
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
