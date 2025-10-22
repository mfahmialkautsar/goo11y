package integration

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultPollInterval = 100 * time.Millisecond

// EnvOrDefault returns the value of key or fallback if the key is unset.
func EnvOrDefault(key, fallback string) string {
	if value := os.Getenv(key); strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

// WaitUntil polls fn until it returns true or the context is done.
func WaitUntil(ctx context.Context, interval time.Duration, fn func(context.Context) (bool, error)) error {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		done, err := fn(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// CheckReachable returns an error if the target cannot be reached within the context deadline.
func CheckReachable(ctx context.Context, rawURL string) error {
	if rawURL == "" {
		return errors.New("empty url")
	}

	dialer := &net.Dialer{Timeout: time.Second}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Timeout: time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// WaitForEmptyDir waits for dir to contain no entries.
func WaitForEmptyDir(ctx context.Context, dir string, interval time.Duration) error {
	return WaitUntil(ctx, interval, func(context.Context) (bool, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, err
		}
		return len(entries) == 0, nil
	})
}
