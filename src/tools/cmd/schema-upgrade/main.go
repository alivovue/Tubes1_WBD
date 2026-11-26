// schema-upgrade is an explicit offline metadata upgrade, never application startup.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"schakker_schemas/platform"
)

func main() {
	if err := run(); err != nil {
		// Driver errors can contain DSNs or row/SQL values. Keep them off console.
		fmt.Fprintln(os.Stderr, "Metadata upgrade failed. Stop services, check owner privileges, resolve unfinished jobs, and verify original target receipts. Target migration SQL was not replayed.")
		os.Exit(1)
	}
}
func run() error {
	apply := flag.Bool("apply", false, "Commit the metadata upgrade (default: verify and roll back)")
	flag.Parse()
	url := os.Getenv("SCHAKKER_UPGRADE_DATABASE_URL")
	if url == "" {
		return errors.New("metadata owner URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer db.Close()
	urls := map[string]string{}
	if path := os.Getenv("SCHAKKER_UPGRADE_TARGETS_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &urls); err != nil {
			return err
		}
	}
	targets := map[int64]*sql.DB{}
	defer func() {
		for _, target := range targets {
			target.Close()
		}
	}()
	report, err := platform.Upgrade(ctx, db, platform.UpgradeOptions{Apply: *apply, Verify: func(ctx context.Context, e platform.LegacyExecution, namespace string) (platform.LegacyReceipt, error) {
		target := targets[e.DatabaseID]
		if target == nil {
			url := urls[strconv.FormatInt(e.DatabaseID, 10)]
			if url == "" {
				return platform.LegacyReceipt{}, errors.New("missing original target")
			}
			var err error
			target, err = sql.Open("pgx", url)
			if err != nil {
				return platform.LegacyReceipt{}, err
			}
			targets[e.DatabaseID] = target
		}
		tx, err := target.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return platform.LegacyReceipt{}, err
		}
		defer tx.Rollback()
		var receipt platform.LegacyReceipt
		err = tx.QueryRowContext(ctx, `SELECT version,sql_hash,applied_at FROM _schakker.migration_receipts WHERE namespace=$1 AND attempt_id=$2`, namespace, e.ID).Scan(&receipt.Version, &receipt.SQLHash, &receipt.CommittedAt)
		return receipt, err
	}})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
