package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement"
	"tubes1_wbd/database/dao"
)

const MariaDBName = "schakker_mock_mariadb"

//go:embed mariadb/employees.sql
var mariaDBSQL string

type MariaDB struct {
	Directory, Socket, DatabaseName string
	Port                            int
	password, admin, id             string
	command                         *exec.Cmd
	stopped                         chan struct{}
	once                            sync.Once
}

func openMariaDB(ctx context.Context, directory string) (*MariaDB, error) {
	server, installer, err := mariaDBBinaries()
	if err != nil {
		return nil, err
	}
	owner, err := user.Current()
	if err != nil {
		return nil, err
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(directory))
	id := hex.EncodeToString(sum[:6])
	socketDirectory := filepath.Join(directory, "socket")
	if len(socketDirectory) > 80 {
		socketDirectory = filepath.Join(os.TempDir(), "schakker-maria-"+strconv.Itoa(os.Getuid())+"-"+id)
	}
	if err := os.MkdirAll(socketDirectory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(socketDirectory, 0700); err != nil {
		return nil, err
	}
	m := &MariaDB{Directory: directory, Socket: filepath.Join(socketDirectory, "server.sock"), DatabaseName: MariaDBName + "_" + id, admin: owner.Username, id: id}
	m.Port, err = mariaDBPort(directory)
	if err != nil {
		return nil, err
	}
	dataDirectory := filepath.Join(directory, "data")
	m.password, err = mariaDBPassword(directory, dataDirectory)
	if err != nil {
		return nil, err
	}
	basedir := filepath.Dir(filepath.Dir(server))
	if _, err := os.Stat(filepath.Join(dataDirectory, "mysql")); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(dataDirectory); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("mock MariaDB data directory is incomplete; inspect it before starting")
		}
		staging, err := os.MkdirTemp(directory, "init-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(staging)
		command := exec.CommandContext(ctx, installer, "--no-defaults", "--basedir="+basedir, "--datadir="+staging, "--auth-root-authentication-method=socket", "--auth-root-socket-user="+owner.Username, "--skip-test-db", "--skip-name-resolve", "--innodb-buffer-pool-size=32M", "--innodb-log-file-size=8M")
		output, err := command.CombinedOutput()
		log := filepath.Join(directory, "init.log")
		if writeErr := os.WriteFile(log, output, 0600); writeErr != nil {
			return nil, writeErr
		}
		if err != nil {
			return nil, fmt.Errorf("mock MariaDB initialization failed; inspect %s", log)
		}
		if err := os.Rename(staging, dataDirectory); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	admin, err := m.openAdmin()
	if err != nil {
		return nil, err
	}
	defer admin.Close()
	if err := admin.PingContext(ctx); err != nil {
		if err := m.start(ctx, server, basedir, dataDirectory, admin); err != nil {
			m.Close()
			return nil, err
		}
	}
	fail := func(err error) (*MariaDB, error) { m.Close(); return nil, err }
	var actualDirectory, version string
	if err := admin.QueryRowContext(ctx, `SELECT @@datadir,VERSION()`).Scan(&actualDirectory, &version); err != nil {
		return fail(err)
	}
	if filepath.Clean(actualDirectory) != dataDirectory || !strings.Contains(strings.ToLower(version), "mariadb") {
		return fail(errors.New("mock MariaDB socket belongs to a different database instance"))
	}
	connection, err := admin.Conn(ctx)
	if err != nil {
		return fail(err)
	}
	defer connection.Close()
	queries := []string{
		"CREATE DATABASE IF NOT EXISTS `" + m.DatabaseName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		"CREATE USER IF NOT EXISTS '" + managedRole + "'@'127.0.0.1' IDENTIFIED BY '" + m.password + "'",
		"GRANT ALL PRIVILEGES ON `" + m.DatabaseName + "`.* TO '" + managedRole + "'@'127.0.0.1'",
		"USE `" + m.DatabaseName + "`",
	}
	for _, query := range queries {
		if _, err := connection.ExecContext(ctx, query); err != nil {
			return fail(errors.New("mock MariaDB database or managed account could not be prepared"))
		}
	}
	seeded := filepath.Join(directory, ".seeded")
	if _, err := os.Stat(seeded); errors.Is(err, os.ErrNotExist) {
		if _, err := connection.ExecContext(ctx, mariaDBSQL); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(seeded, []byte("schakker-mariadb-fixture-v1\n"), 0600); err != nil {
			return fail(err)
		}
	} else if err != nil {
		return fail(err)
	}
	return m, nil
}

func (m *MariaDB) openAdmin() (*sql.DB, error) {
	config := mysql.NewConfig()
	config.Net, config.Addr, config.User = "unix", m.Socket, m.admin
	config.Timeout, config.ReadTimeout, config.WriteTimeout = time.Second, 5*time.Second, 5*time.Second
	config.MultiStatements = true
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	return db, nil
}

func (m *MariaDB) start(ctx context.Context, server, basedir, dataDirectory string, admin *sql.DB) error {
	logPath := filepath.Join(m.Directory, "server.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	command := exec.Command(server, "--no-defaults", "--basedir="+basedir, "--datadir="+dataDirectory, "--socket="+m.Socket, "--pid-file="+filepath.Join(m.Directory, "server.pid"), "--port="+strconv.Itoa(m.Port), "--bind-address=127.0.0.1", "--skip-name-resolve", "--max-connections=20", "--innodb-buffer-pool-size=32M", "--innodb-log-file-size=8M", "--performance-schema=OFF", "--log-error="+logPath)
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		log.Close()
		return err
	}
	m.command, m.stopped = command, make(chan struct{})
	go func() { _ = command.Wait(); _ = log.Close(); close(m.stopped) }()
	ready, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := admin.PingContext(ready); err == nil {
			return nil
		}
		select {
		case <-ready.Done():
			return fmt.Errorf("mock MariaDB did not become ready; inspect %s", logPath)
		case <-m.stopped:
			return fmt.Errorf("mock MariaDB did not start; inspect %s", logPath)
		case <-ticker.C:
		}
	}
}

func (m *MariaDB) Close() {
	m.once.Do(func() {
		if m.command == nil {
			return
		}
		_ = m.command.Process.Signal(syscall.SIGTERM)
		select {
		case <-m.stopped:
		case <-time.After(20 * time.Second):
			_ = m.command.Process.Kill()
			<-m.stopped
		}
	})
}

func (m *MariaDB) Connection() dbmanagement.ConnectionInput {
	return dbmanagement.ConnectionInput{DBMS: "mariadb", Name: "Mock MariaDB Database " + m.id, Host: "127.0.0.1", Port: m.Port, DatabaseName: m.DatabaseName, Username: managedRole, Password: m.password, SSLMode: "disable", Active: true}
}

func (m *MariaDB) AttachManaged(ctx context.Context, store *dao.Store, auth *access.Service, databases *dbmanagement.Service) error {
	return attachManaged(ctx, store, auth, databases, m.Connection())
}

func mariaDBPassword(directory, dataDirectory string) (string, error) {
	filename := filepath.Join(directory, "managed.key")
	password, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(dataDirectory); !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("mock MariaDB credentials are missing; restore mariadb/managed.key")
		}
		generated := make([]byte, 32)
		if _, err := rand.Read(generated); err != nil {
			return "", err
		}
		password = []byte(hex.EncodeToString(generated))
		if err := os.WriteFile(filename, password, 0600); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(password))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("mock MariaDB credentials are invalid")
	}
	return value, nil
}

func mariaDBPort(directory string) (int, error) {
	filename := filepath.Join(directory, "local.json")
	var settings struct{ Port int }
	raw, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		settings.Port = listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		raw, _ = json.Marshal(settings)
		if err := os.WriteFile(filename, raw, 0600); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	} else if json.Unmarshal(raw, &settings) != nil || settings.Port < 1 || settings.Port > 65535 {
		return 0, errors.New("mock MariaDB configuration is invalid")
	}
	return settings.Port, nil
}

func mariaDBBinaries() (string, string, error) {
	directories := []string{"/usr/sbin", "/usr/bin", "/usr/local/sbin", "/usr/local/bin"}
	if configured := os.Getenv("SCHAKKER_MARIADB_BIN"); configured != "" {
		directories = []string{configured, filepath.Join(configured, "..", "bin"), filepath.Join(configured, "..", "sbin"), filepath.Join(configured, "..", "scripts")}
	} else {
		directories = append(filepath.SplitList(os.Getenv("PATH")), directories...)
	}
	find := func(names ...string) string {
		for _, directory := range directories {
			for _, name := range names {
				path := filepath.Join(directory, name)
				info, err := os.Stat(path)
				if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
					resolved, err := filepath.EvalSymlinks(path)
					if err == nil {
						return resolved
					}
				}
			}
		}
		return ""
	}
	server, installer := find("mariadbd"), find("mariadb-install-db", "mysql_install_db")
	if server == "" || installer == "" {
		return "", "", errors.New("install MariaDB server tools (mariadbd and mariadb-install-db), or set SCHAKKER_MARIADB_BIN to their bin directory to use the mock MariaDB database")
	}
	return server, installer, nil
}
