//go:build !windows

package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
)

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
	if err := m.prepare(ctx, admin, dataDirectory); err != nil {
		m.Close()
		return nil, err
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

func (m *MariaDB) serverCommand(server, basedir, dataDirectory, logPath string) *exec.Cmd {
	command := exec.Command(server, "--no-defaults", "--basedir="+basedir, "--datadir="+dataDirectory, "--socket="+m.Socket, "--pid-file="+filepath.Join(m.Directory, "server.pid"), "--port="+strconv.Itoa(m.Port), "--bind-address=127.0.0.1", "--skip-name-resolve", "--max-connections=20", "--innodb-buffer-pool-size=32M", "--innodb-log-file-size=8M", "--performance-schema=OFF", "--log-error="+logPath)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return command
}

func (m *MariaDB) stopProcess() {
	_ = m.command.Process.Signal(syscall.SIGTERM)
}

func sameMariaDBDirectory(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
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
