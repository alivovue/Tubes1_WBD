package setup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func OpenLocal(ctx context.Context, directory string) (*sql.DB, func(), error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, nil, err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, nil, err
	}
	binaries, err := postgresBinaries(ctx)
	if err != nil {
		return nil, nil, err
	}
	data := filepath.Join(directory, "postgres")
	socket := filepath.Join(directory, "socket")
	if len(socket) > 85 {
		return nil, nil, errors.New("the project path is too long for a PostgreSQL socket; set SCHAKKER_STATE_DIR to a shorter private directory")
	}
	if err := os.MkdirAll(socket, 0700); err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(filepath.Join(data, "PG_VERSION")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		if _, err := os.Stat(data); !errors.Is(err, os.ErrNotExist) {
			return nil, nil, errors.New("local PostgreSQL data directory is incomplete; inspect it before starting")
		}
		staging, err := os.MkdirTemp(directory, "postgres-init-")
		if err != nil {
			return nil, nil, err
		}
		defer os.RemoveAll(staging)
		command := exec.CommandContext(ctx, filepath.Join(binaries, "initdb"), "-D", staging, "-U", "schakker", "--auth-local=trust", "--auth-host=reject", "--no-locale", "--encoding=UTF8")
		if _, err := command.CombinedOutput(); err != nil {
			return nil, nil, errors.New("local PostgreSQL initialization failed; run as a regular user with PostgreSQL server tools installed")
		}
		escapedSocket := strings.ReplaceAll(strings.ReplaceAll(socket, `\`, `\\`), "'", "''")
		settings := "\nlisten_addresses = ''\nunix_socket_directories = '" + escapedSocket + "'\nunix_socket_permissions = 0700\nport = 5432\n"
		config, err := os.OpenFile(filepath.Join(staging, "postgresql.conf"), os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return nil, nil, err
		}
		_, writeErr := config.WriteString(settings)
		closeErr := config.Close()
		if writeErr != nil {
			return nil, nil, writeErr
		}
		if closeErr != nil {
			return nil, nil, closeErr
		}
		if err := os.Rename(staging, data); err != nil {
			return nil, nil, err
		}
	}
	control := filepath.Join(binaries, "pg_ctl")
	started := exec.CommandContext(ctx, control, "-D", data, "status").Run() != nil
	stop := func() {
		if !started {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, control, "-D", data, "-m", "fast", "-w", "-t", "15", "stop").Run()
	}
	if started {
		if _, err := exec.CommandContext(ctx, control, "-D", data, "-l", filepath.Join(directory, "postgres.log"), "-w", "-t", "15", "start").CombinedOutput(); err != nil {
			return nil, nil, fmt.Errorf("local PostgreSQL did not start; inspect %s", filepath.Join(directory, "postgres.log"))
		}
	}
	fail := func(err error) (*sql.DB, func(), error) { stop(); return nil, nil, err }
	connection := func(database string) string {
		values := url.Values{"host": {socket}, "port": {"5432"}, "sslmode": {"disable"}, "connect_timeout": {"5"}}
		return (&url.URL{Scheme: "postgres", User: url.User("schakker"), Path: "/" + database, RawQuery: values.Encode()}).String()
	}
	admin, err := Open(ctx, connection("postgres"))
	if err != nil {
		return fail(err)
	}
	var exists bool
	err = admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='schakker_platform')`).Scan(&exists)
	if err == nil && !exists {
		_, err = admin.ExecContext(ctx, `CREATE DATABASE schakker_platform WITH TEMPLATE template0 ENCODING 'UTF8'`)
	}
	admin.Close()
	if err != nil {
		return fail(errors.New("local website database could not be created"))
	}
	db, err := Open(ctx, connection("schakker_platform"))
	if err != nil {
		return fail(err)
	}
	return db, stop, nil
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
