package intervalsicu

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewClientValidation(t *testing.T) {
	_, err := NewClient("", "athlete")
	if err == nil {
		t.Fatalf("expected error for empty api key")
	}
	_, err = NewClient("key", "")
	if err == nil {
		t.Fatalf("expected error for empty athlete id")
	}
}

func TestGetAthleteInfo(t *testing.T) {
	loadDotEnv(t, ".env")

	apiKey := strings.TrimSpace(os.Getenv("INTERVALS_API_KEY"))
	athleteID := strings.TrimSpace(os.Getenv("INTERVALS_ATHLETE_ID"))
	if apiKey == "" || athleteID == "" {
		t.Skip("INTERVALS_API_KEY or INTERVALS_ATHLETE_ID not set")
	}

	opts := []Option{}
	if baseURL := strings.TrimSpace(os.Getenv("INTERVALS_BASE_URL")); baseURL != "" {
		opts = append(opts, WithBaseURL(baseURL))
	}

	client, err := NewClient(apiKey, athleteID, opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	athlete, err := client.GetAthleteInfo(ctx)
	if err != nil {
		t.Fatalf("GetAthleteInfo: %v", err)
	}
	if athlete == nil {
		t.Fatalf("GetAthleteInfo returned nil")
	}
}

func loadDotEnv(t *testing.T, name string) {
	path := filepath.Clean(name)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("open %s: %v", name, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "\"'")
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
}
