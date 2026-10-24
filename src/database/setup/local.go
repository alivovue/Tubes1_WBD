package setup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

func StartLocalCluster(ctx context.Context, directory string, options LocalOptions) (*LocalCluster, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	binaries, err := postgresBinaries(ctx)
	if err != nil {
		return nil, err
	}
	if options.TCPRole != "" && !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`).MatchString(options.TCPRole) {
		return nil, errors.New("invalid local database role")
	}
	settingsPath := filepath.Join(directory, "local.json")
	var settings localSettings
	raw, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if json.Unmarshal(raw, &settings) != nil || settings.Socket == "" || settings.Port < 1 || settings.Port > 65535 || settings.TCPRole != options.TCPRole {
			return nil, errors.New("local PostgreSQL configuration is invalid or uses a different connection mode")
		}
	case errors.Is(err, os.ErrNotExist):
		settings = localSettings{Socket: filepath.Join(directory, "socket"), Port: 5432, TCPRole: options.TCPRole}
		if len(settings.Socket) > 85 {
			hash := sha256.Sum256([]byte(directory))
			settings.Socket = filepath.Join(os.TempDir(), "schakker-socket-"+strconv.Itoa(os.Getuid())+"-"+hex.EncodeToString(hash[:8]))
		}
		if options.TCPRole != "" {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return nil, err
			}
			settings.Port = listener.Addr().(*net.TCPAddr).Port
			listener.Close()
		}
		raw, _ = json.Marshal(settings)
		if err := os.WriteFile(settingsPath, raw, 0600); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	if err := os.MkdirAll(settings.Socket, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(settings.Socket, 0700); err != nil {
		return nil, err
	}
	data := filepath.Join(directory, "postgres")
	if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if _, err := os.Stat(data); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("local PostgreSQL data directory is incomplete; inspect it before starting")
		}
		staging, err := os.MkdirTemp(directory, "postgres-init-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(staging)
		command := exec.CommandContext(ctx, filepath.Join(binaries, "initdb"), "-D", staging, "-U", "schakker", "--auth-local=trust", "--auth-host=reject", "--no-locale", "--encoding=UTF8")
		if _, err := command.CombinedOutput(); err != nil {
			return nil, errors.New("local PostgreSQL initialization failed; run as a regular user with PostgreSQL server tools installed")
		}
		if err := os.Rename(staging, data); err != nil {
			return nil, err
		}
	}
	cluster := &LocalCluster{Directory: directory, Socket: settings.Socket, Port: settings.Port, control: filepath.Join(binaries, "pg_ctl")}
	if exec.CommandContext(ctx, cluster.control, "-D", data, "status").Run() == nil {
		return cluster, nil
	}
	escapedSocket := strings.ReplaceAll(strings.ReplaceAll(settings.Socket, `\`, `\\`), "'", "''")
	listen := ""
	if settings.TCPRole != "" {
		listen = "127.0.0.1"
	}
	config, err := os.OpenFile(filepath.Join(data, "postgresql.conf"), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := fmt.Fprintf(config, "\nlisten_addresses = '%s'\nunix_socket_directories = '%s'\nunix_socket_permissions = 0700\nport = %d\n", listen, escapedSocket, settings.Port)
	closeErr := config.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if settings.TCPRole != "" {
		hba := "local all all trust\nhost all " + settings.TCPRole + " 127.0.0.1/32 scram-sha-256\nhost all all 0.0.0.0/0 reject\nhost all all ::0/0 reject\n"
		if err := os.WriteFile(filepath.Join(data, "pg_hba.conf"), []byte(hba), 0600); err != nil {
			return nil, err
		}
	}
	if _, err := exec.CommandContext(ctx, cluster.control, "-D", data, "-l", filepath.Join(directory, "postgres.log"), "-w", "-t", "15", "start").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("local PostgreSQL did not start; inspect %s", filepath.Join(directory, "postgres.log"))
	}
	cluster.started = true
	return cluster, nil
}
func (c *LocalCluster) DSN(database string) string {
	values := url.Values{"host": {c.Socket}, "port": {strconv.Itoa(c.Port)}, "sslmode": {"disable"}, "connect_timeout": {"5"}}
	return (&url.URL{Scheme: "postgres", User: url.User("schakker"), Path: "/" + database, RawQuery: values.Encode()}).String()
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

func postgresBinaries(ctx context.Context) (string, error) {
	directories := []string{}
	if configured := os.Getenv("SCHAKKER_POSTGRES_BIN"); configured != "" {
		directories = append(directories, configured)
	}
	if initdb, err := exec.LookPath("initdb"); err == nil {
		directories = append(directories, filepath.Dir(initdb))
	}
	if config, err := exec.LookPath("pg_config"); err == nil {
		lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
		output, err := exec.CommandContext(lookup, config, "--bindir").Output()
		cancel()
		if err == nil {
			directories = append(directories, strings.TrimSpace(string(output)))
		}
	}
	installed, _ := filepath.Glob("/usr/lib/postgresql/*/bin")
	sort.Sort(sort.Reverse(sort.StringSlice(installed)))
	directories = append(directories, installed...)
	for _, directory := range directories {
		valid := true
		for _, binary := range []string{"initdb", "pg_ctl", "postgres"} {
			info, err := os.Stat(filepath.Join(directory, binary))
			if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
				valid = false
				break
			}
		}
		if valid {
			return directory, nil
		}
	}
	return "", errors.New("install PostgreSQL server tools once, or set SCHAKKER_POSTGRES_BIN to their bin directory; an existing database can also be used with SCHAKKER_DATABASE_URL")
}
