package setup

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type LocalOptions struct{ TCPRole string }
type LocalCluster struct {
	Directory string
	Socket    string
	Port      int
	password  string
	control   string
	started   bool
	once      sync.Once
}
type localSettings struct {
	Socket  string
	Port    int
	TCPRole string
}

func OpenLocal(ctx context.Context, directory string) (*sql.DB, func(), error) {
	cluster, err := StartLocalCluster(ctx, directory, LocalOptions{})
	if err != nil {
		return nil, nil, err
	}
	if _, err := cluster.EnsureDatabase(ctx, "schakker_platform", "", ""); err != nil {
		cluster.Close()
		return nil, nil, err
	}
	db, err := cluster.Open(ctx, "schakker_platform")
	if err != nil {
		cluster.Close()
		return nil, nil, err
	}
	return db, cluster.Close, nil
}

func (c *LocalCluster) DSN(database string) string {
	return localDSN(c, database)
}
func (c *LocalCluster) Open(ctx context.Context, database string) (*sql.DB, error) {
	return Open(ctx, c.DSN(database))
}
func (c *LocalCluster) EnsureDatabase(ctx context.Context, name, owner, template string) (bool, error) {
	if name == "" || len(name) > 63 || strings.ContainsRune(name, 0) {
		return false, errors.New("invalid database name")
	}
	admin, err := c.Open(ctx, "postgres")
	if err != nil {
		return false, err
	}
	defer admin.Close()
	var exists bool
	if err := admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)`, name).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	query := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if template != "" {
		query += " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	} else {
		query += " TEMPLATE template0 ENCODING 'UTF8'"
	}
	if owner != "" {
		query += " OWNER " + pgx.Identifier{owner}.Sanitize()
	}
	if _, err := admin.ExecContext(ctx, query); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "42P04" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
func (c *LocalCluster) Close() {
	c.once.Do(func() {
		if !c.started {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, c.control, "-D", filepath.Join(c.Directory, "postgres"), "-m", "fast", "-w", "-t", "15", "stop").Run()
	})
}
