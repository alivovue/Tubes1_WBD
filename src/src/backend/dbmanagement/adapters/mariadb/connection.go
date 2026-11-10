package mariadb

import (
	"context"
	"database/sql"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"tubes1_wbd/backend/dbmanagement/adapters"
	"tubes1_wbd/backend/dbmanagement/adapters/mariadb/queryplan"
)

type Adapter struct{}
type quietLogger struct{}

func (quietLogger) Print(...any) {}
func New() Adapter               { return Adapter{} }
func (Adapter) Name() string     { return "mariadb" }
func (Adapter) Connect(ctx context.Context, in adapters.Connection) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(in.Host, strconv.Itoa(in.Port))
	cfg.DBName = in.DatabaseName
	cfg.User = in.Username
	cfg.Passwd = in.Password
	cfg.Timeout = in.ConnectTimeout
	cfg.ReadTimeout = in.QueryTimeout
	cfg.WriteTimeout = in.QueryTimeout
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false
	cfg.InterpolateParams = false
	cfg.Collation = "utf8mb4_unicode_ci"
	cfg.Logger = quietLogger{}
	cfg.Params = map[string]string{"sql_mode": "'STRICT_ALL_TABLES,NO_BACKSLASH_ESCAPES'", "time_zone": "'+00:00'"}
	switch in.SSLMode {
	case "disable":
		cfg.TLSConfig = "false"
	case "require":
		cfg.TLSConfig = "skip-verify"
	case "verify-full":
		cfg.TLSConfig = "true"
	default:
		return nil, adapters.ErrInvalid
	}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, adapters.ErrConnection
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	check, cancel := context.WithTimeout(ctx, in.ConnectTimeout)
	defer cancel()
	var version, name string
	if err := db.QueryRowContext(check, `SELECT VERSION(),DATABASE()`).Scan(&version, &name); err != nil {
		db.Close()
		return nil, adapters.ErrConnection
	}
	if !strings.Contains(strings.ToLower(version), "mariadb") || name != in.DatabaseName {
		db.Close()
		return nil, adapters.ErrUnsupported
	}
	var major, minor int
	parts := strings.Split(version, ".")
	if len(parts) > 1 {
		major, _ = strconv.Atoi(parts[0])
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 10 || major == 10 && minor < 6 {
		db.Close()
		return nil, adapters.ErrUnsupported
	}
	return db, nil
}
func (Adapter) QuoteIdentifier(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", adapters.ErrInvalid
	}
	quoted := make([]string, len(parts))
	for i, part := range parts {
		if !adapters.ValidIdentifier(part, 64) {
			return "", adapters.ErrInvalid
		}
		quoted[i] = "`" + strings.ReplaceAll(part, "`", "``") + "`"
	}
	return strings.Join(quoted, "."), nil
}
func (Adapter) QueryPlans(ctx context.Context, q adapters.Querier, statements []adapters.Statement) ([]adapters.Plan, error) {
	name, err := databaseName(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, statement := range statements {
		if err := adapters.ValidateMariaReferences(name, statement); err != nil {
			return nil, err
		}
	}
	return queryplan.Analyze(ctx, q, statements)
}

func (Adapter) DefaultPort() int { return 3306 }
