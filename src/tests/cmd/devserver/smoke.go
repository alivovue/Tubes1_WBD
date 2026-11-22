package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

type apiClient struct {
	base, csrf string
	client     *http.Client
}

func (client *apiClient) request(ctx context.Context, method, path string, body any, result any) error {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.base+"/api"+path, input)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", client.base)
		request.Header.Set("X-CSRF-Token", client.csrf)
		if strings.HasSuffix(path, "/enqueue") {
			request.Header.Set("Idempotency-Key", "dev-check-"+randomHex(12))
		}
	}
	response, err := client.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&failure)
		return fmt.Errorf("%s %s returned %d: %s", method, path, response.StatusCode, failure.Error)
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(result)
	}
	return nil
}
func login(ctx context.Context, base, username, password string) (*apiClient, error) {
	jar, _ := cookiejar.New(nil)
	client := &apiClient{base: base, client: &http.Client{Jar: jar, Timeout: 15 * time.Second}}
	var session struct{ CSRF string }
	if err := client.request(ctx, "GET", "/session", nil, &session); err != nil {
		return nil, err
	}
	client.csrf = session.CSRF
	if err := client.request(ctx, "POST", "/login", map[string]string{"username": username, "password": password}, nil); err != nil {
		return nil, err
	}
	if err := client.request(ctx, "GET", "/session", nil, &session); err != nil {
		return nil, err
	}
	client.csrf = session.CSRF
	return client, nil
}

type checkReview struct {
	ID     int64 `json:"id"`
	Groups []struct {
		ID         int64  `json:"group_id"`
		Permission string `json:"permission"`
	} `json:"required_groups"`
}

func smoke(ctx context.Context, config *configuration) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	admin, err := login(ctx, "http://127.0.0.1:3001", "dev.admin", config.Password)
	if err != nil {
		return err
	}
	if err := admin.request(ctx, "GET", "/admin/groups", nil, nil); err != nil {
		return err
	}
	editor, err := login(ctx, "http://127.0.0.1:3000", "dev.editor", config.Password)
	if err != nil {
		return err
	}
	reader, err := login(ctx, "http://127.0.0.1:3000", "dev.reader", config.Password)
	if err != nil {
		return err
	}
	table := "dev_startup_check_" + randomHex(6)
	var created struct {
		Definition struct {
			RevisionID int64 `json:"revision_id"`
			Generation int64 `json:"expected_generation"`
		} `json:"definition"`
	}
	if err := editor.request(ctx, "POST", "/migration-definitions", map[string]any{"database_id": config.DatabaseID, "title": "Development startup verification", "up_sql": "CREATE TABLE public." + table + "(id BIGINT PRIMARY KEY)", "down_sql": "DROP TABLE public." + table, "irreversible": false, "irreversible_reason": ""}, &created); err != nil {
		return err
	}
	var reviewed struct {
		Review checkReview `json:"review"`
	}
	if err := editor.request(ctx, "POST", "/migration-reviews", map[string]any{"revision_id": created.Definition.RevisionID, "expected_generation": created.Definition.Generation, "action": "up"}, &reviewed); err != nil {
		return err
	}
	if err := approveAndExecute(ctx, editor, reader, reviewed.Review); err != nil {
		return err
	}
	var history struct {
		Executions []struct {
			ID          string `json:"id"`
			RevisionID  int64  `json:"revision_id"`
			Generation  int64  `json:"expected_generation"`
			CanRollback bool   `json:"can_rollback"`
		} `json:"executions"`
	}
	path := fmt.Sprintf("/migrations/history?database_id=%d", config.DatabaseID)
	if err := editor.request(ctx, "GET", path, nil, &history); err != nil {
		return err
	}
	for _, execution := range history.Executions {
		if execution.RevisionID == created.Definition.RevisionID && execution.CanRollback {
			if err := editor.request(ctx, "POST", "/migrations/"+execution.ID+"/rollback-reviews", map[string]any{"expected_generation": execution.Generation}, &reviewed); err != nil {
				return err
			}
			return approveAndExecute(ctx, editor, reader, reviewed.Review)
		}
	}
	return fmt.Errorf("worker commit was not present in committed history")
}
func approveAndExecute(ctx context.Context, editor, reader *apiClient, review checkReview) error {
	if len(review.Groups) != 2 {
		return fmt.Errorf("expected both local approval groups, got %d", len(review.Groups))
	}
	route := fmt.Sprintf("/migration-reviews/%d", review.ID)
	for _, group := range review.Groups {
		client := editor
		if group.Permission == "view_only" {
			client = reader
		}
		if err := client.request(ctx, "POST", route+"/decisions", map[string]any{"group_id": group.ID, "decision": "approve", "note": "Local development startup check"}, nil); err != nil {
			return err
		}
	}
	var queued struct {
		ID string `json:"job_id"`
	}
	if err := editor.request(ctx, "POST", route+"/enqueue", map[string]any{}, &queued); err != nil {
		return err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var result struct {
				Job struct {
					Status string `json:"status"`
					Error  string `json:"error"`
				} `json:"job"`
			}
			if err := editor.request(ctx, "GET", "/migration-jobs/"+queued.ID, nil, &result); err != nil {
				return err
			}
			if result.Job.Status == "succeeded" {
				return nil
			}
			if result.Job.Status != "pending" && result.Job.Status != "running" {
				return fmt.Errorf("worker job ended %s: %s", result.Job.Status, result.Job.Error)
			}
		}
	}
}
