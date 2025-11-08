package goo11y

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

func burnCPU(duration time.Duration) {
	deadline := time.Now().Add(duration)
	sum := 0.0
	for time.Now().Before(deadline) {
		for i := 1; i < 5000; i++ {
			sum += math.Sqrt(float64(i))
		}
	}
	_ = sum
}

func waitForTelemetryFileEntry(t *testing.T, path, expectedMessage string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
		for i := len(lines) - 1; i >= 0; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(line, &payload); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if fmt.Sprint(payload["message"]) == expectedMessage {
				return payload
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("log message %q not found in %s", expectedMessage, path)
	return nil
}
