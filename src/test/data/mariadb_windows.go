package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
)

func openMariaDB(ctx context.Context, directory string) (*MariaDB, error) {
	server, installer, err := mariaDBBinaries()
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
	sum := sha256.Sum256([]byte(strings.ToLower(directory)))
	id := hex.EncodeToString(sum[:6])
	m := &MariaDB{Directory: directory, DatabaseName: MariaDBName + "_" + id, admin: "root", id: id}
	m.Port, err = mariaDBPort(directory)
	if err != nil {
		return nil, err
	}
	dataDirectory := filepath.Join(directory, "data")
	m.password, err = mariaDBPassword(directory, dataDirectory)
	if err != nil {
		return nil, err
	}
	m.adminPassword, err = mariaDBSecret(directory, dataDirectory, "admin.key")
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "mysql")); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(dataDirectory); !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("mock MariaDB data directory is incomplete; inspect it before starting")
		}
		staging, err := os.MkdirTemp(directory, "init-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(staging)
		template := filepath.Join(directory, "bootstrap.ini")
		if err := os.WriteFile(template, []byte("[mysqld]\ninnodb-buffer-pool-size=32M\ninnodb-log-file-size=8M\nperformance-schema=OFF\n"), 0600); err != nil {
			return nil, err
		}
		defer os.Remove(template)
		command := exec.CommandContext(ctx, installer, "--datadir="+staging, "--password="+m.adminPassword, "--port="+strconv.Itoa(m.Port), "--config="+template, "--silent")
		output, err := command.CombinedOutput()
		log := filepath.Join(directory, "init.log")
		if writeErr := os.WriteFile(log, []byte(strings.ReplaceAll(string(output), m.adminPassword, "[redacted]")), 0600); writeErr != nil {
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
		if err := m.start(ctx, server, filepath.Dir(filepath.Dir(server)), dataDirectory, admin); err != nil {
			m.Close()
			return nil, err
		}
	}
	if err := m.prepare(ctx, admin, dataDirectory); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

func (m *MariaDB) openAdmin() (*sql.DB, error) {
	config := mysql.NewConfig()
	config.Net, config.Addr, config.User = "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(m.Port)), m.admin
	config.Passwd = m.adminPassword
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

func (m *MariaDB) serverCommand(server, basedir, dataDirectory, logPath string) *exec.Cmd {
	command := exec.Command(server, "--no-defaults", "--console", "--basedir="+basedir, "--datadir="+dataDirectory, "--pid-file="+filepath.Join(m.Directory, "server.pid"), "--port="+strconv.Itoa(m.Port), "--bind-address=127.0.0.1", "--skip-name-resolve", "--max-connections=20", "--innodb-buffer-pool-size=32M", "--innodb-log-file-size=8M", "--performance-schema=OFF", "--log-error="+logPath)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return command
}

func (m *MariaDB) stopProcess() {
	admin, err := m.openAdmin()
	if err != nil {
		return
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = admin.ExecContext(ctx, "SHUTDOWN")
}

func sameMariaDBDirectory(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func mariaDBBinaries() (string, string, error) {
	directories := filepath.SplitList(os.Getenv("PATH"))
	if configured := os.Getenv("SCHAKKER_MARIADB_BIN"); configured != "" {
		directories = []string{configured, filepath.Join(configured, "bin"), filepath.Join(configured, "..", "bin")}
	} else {
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root == "" {
				continue
			}
			installed, _ := filepath.Glob(filepath.Join(root, "MariaDB*", "bin"))
			sort.Sort(sort.Reverse(sort.StringSlice(installed)))
			directories = append(directories, installed...)
		}
	}
	find := func(names ...string) string {
		for _, directory := range directories {
			for _, name := range names {
				path := filepath.Join(directory, name)
				if info, err := os.Stat(path); err == nil && !info.IsDir() {
					return path
				}
			}
		}
		return ""
	}
	server, installer := find("mariadbd.exe", "mysqld.exe"), find("mariadb-install-db.exe", "mysql_install_db.exe")
	if server == "" || installer == "" {
		return "", "", errors.New("install MariaDB server tools for Windows (mariadbd.exe and mariadb-install-db.exe), or set SCHAKKER_MARIADB_BIN to their bin directory to use the mock MariaDB database")
	}
	return server, installer, nil
}
