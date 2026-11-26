package engines

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"tubes1_wbd/backend/dbmanagement/adapters"
)

var ErrJobUncertain = errors.New("target receipt requires reconciliation")
var ErrUnsafeCompensation = errors.New("batch compensation conflicts with current data")

type BatchSpec struct {
	Schema       string `json:"schema"`
	Table        string `json:"table"`
	Key          string `json:"key"`
	Column       string `json:"column"`
	Delta        int64  `json:"delta"`
	Size         int    `json:"size"`
	ExpectedRows int64  `json:"expected_rows"`
}
type JobProgress struct {
	Batches         int64  `json:"committed_batches"`
	Rows            int64  `json:"committed_rows"`
	Cursor          string `json:"last_cursor"`
	Total           int64  `json:"total_rows"`
	RevertedBatches int64  `json:"reverted_batches"`
	RevertedRows    int64  `json:"reverted_rows"`
	State           string `json:"state"`
}
type JobRequest struct {
	JobID, ExecutionID, Namespace, Operation, SQL, DownSQL, SQLHash, ActionHash string
	OriginalJobID                                                               string
	Batch                                                                       *BatchSpec
}
type JobReceipt struct {
	Namespace   string      `json:"namespace"`
	JobID       string      `json:"job_id"`
	ExecutionID string      `json:"execution_id"`
	SQLHash     string      `json:"sql_hash"`
	ActionHash  string      `json:"action_hash"`
	Operation   string      `json:"operation"`
	CommittedAt time.Time   `json:"committed_at"`
	Progress    JobProgress `json:"progress"`
}

func (r JobReceipt) Proof() string { raw, _ := json.Marshal(r); return string(raw) }

type JobHooks struct {
	Guard      func(context.Context) error
	Progress   func(context.Context, JobProgress) error
	Reconciled func()
}

func SQLDigest(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

const batchPrefix = "-- schakker:batch "

var batchIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func BatchSQL(spec BatchSpec) (up, down string, err error) {
	for _, id := range []string{spec.Schema, spec.Table, spec.Key, spec.Column} {
		if !adapters.ValidIdentifier(id, 63) || !batchIdentifier.MatchString(id) || strings.EqualFold(id, "_schakker") {
			return "", "", ErrUnsafeSQL
		}
	}
	if spec.Key == spec.Column || spec.Size < 1 || spec.Size > 10000 || spec.Delta == 0 || spec.ExpectedRows < 0 {
		return "", "", adapters.ErrInvalid
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	raw, _ := json.Marshal(spec)
	header := batchPrefix + string(raw) + "\n"
	base := fmt.Sprintf("UPDATE %s.%s SET %s = %s ", quote(spec.Schema), quote(spec.Table), quote(spec.Column), quote(spec.Column))
	tail := fmt.Sprintf(" $1 WHERE %s = ANY($2::bigint[]);", quote(spec.Key))
	return header + base + "+" + tail, header + base + "-" + tail, nil
}
func ParseBatch(up, down string) (*BatchSpec, error) {
	if !strings.HasPrefix(up, batchPrefix) {
		return nil, nil
	}
	header, _, ok := strings.Cut(up, "\n")
	if !ok {
		return nil, adapters.ErrInvalid
	}
	var spec BatchSpec
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(header, batchPrefix)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&spec) != nil {
		return nil, adapters.ErrInvalid
	}
	expectedUp, expectedDown, err := BatchSQL(spec)
	if err != nil {
		return nil, err
	}
	if up != expectedUp || down != expectedDown {
		return nil, ErrUnsafeSQL
	}
	return &spec, nil
}
