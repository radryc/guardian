package auditanchor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClickHouseConfig locates the append-only audit.events table.
type ClickHouseConfig struct {
	URL      string
	Database string
	User     string
	Password string
}

// Enabled reports whether a ClickHouse endpoint is configured.
func (c ClickHouseConfig) Enabled() bool { return strings.TrimSpace(c.URL) != "" }

// ReadClickHouseDay reads all audit events for one UTC day, ordered by writer
// and sequence. raw_json is preserved as the evidence bytes for verification.
func ReadClickHouseDay(ctx context.Context, cfg ClickHouseConfig, day string) ([]EventRecord, error) {
	if !cfg.Enabled() {
		return nil, fmt.Errorf("clickhouse endpoint is required")
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return nil, fmt.Errorf("invalid day %q: %w", day, err)
	}
	database := strings.TrimSpace(cfg.Database)
	if database == "" {
		database = "audit"
	}
	query := fmt.Sprintf("SELECT event_id, writer_id, seq, timestamp, actor_subject, action, "+
		"resource, partition, decision, prev_hash, hash, raw_json "+
		"FROM %s.events WHERE day = toDate('%s') ORDER BY writer_id, seq FORMAT JSONEachRow",
		database, day)

	u, err := url.Parse(strings.TrimRight(cfg.URL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse clickhouse URL: %w", err)
	}
	params := u.Query()
	params.Set("query", query)
	params.Set("date_time_output_format", "iso")
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if cfg.User != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clickhouse query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("clickhouse query: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	events := make([]EventRecord, 0, 1024)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev EventRecord
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("decode clickhouse audit row: %w", err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan clickhouse audit rows: %w", err)
	}
	return events, nil
}
