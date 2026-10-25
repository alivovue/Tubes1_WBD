package data

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	adminPassword                   string
	command                         *exec.Cmd
	stopped                         chan struct{}
	once                            sync.Once
}

func (m *MariaDB) prepare(ctx context.Context, admin *sql.DB, dataDirectory string) error {
	var actualDirectory, version string
	if err := admin.QueryRowContext(ctx, `SELECT @@datadir,VERSION()`).Scan(&actualDirectory, &version); err != nil {
		return err
	}
	if !sameMariaDBDirectory(actualDirectory, dataDirectory) || !strings.Contains(strings.ToLower(version), "mariadb") {
		return errors.New("mock MariaDB connection belongs to a different database instance")
	}
	connection, err := admin.Conn(ctx)
	if err != nil {
		return err
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
			return errors.New("mock MariaDB database or managed account could not be prepared")
		}
	}
	seeded := filepath.Join(m.Directory, ".seeded")
	if _, err := os.Stat(seeded); errors.Is(err, os.ErrNotExist) {
		if _, err := connection.ExecContext(ctx, mariaDBSQL); err != nil {
			return err
		}
		if err := os.WriteFile(seeded, []byte("schakker-mariadb-fixture-v1\n"), 0600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func (m *MariaDB) start(ctx context.Context, server, basedir, dataDirectory string, admin *sql.DB) error {
	logPath := filepath.Join(m.Directory, "server.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	command := m.serverCommand(server, basedir, dataDirectory, logPath)
	command.Stdout, command.Stderr = log, log
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
		m.stopProcess()
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
	return mariaDBSecret(directory, dataDirectory, "managed.key")
}

func mariaDBSecret(directory, dataDirectory, name string) (string, error) {
	filename := filepath.Join(directory, name)
	password, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(dataDirectory); !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("mock MariaDB credentials are missing; restore mariadb/%s", name)
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

func MariaDBAvailable() bool {
	_, _, err := mariaDBBinaries()
	return err == nil
}
