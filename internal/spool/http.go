package spool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
		if err != nil {
			return err
		}
		for key, values := range req.Header {
			for _, value := range values {
				httpReq.Header.Add(key, value)
			}
		}
		resp, err := client.Do(httpReq)
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
