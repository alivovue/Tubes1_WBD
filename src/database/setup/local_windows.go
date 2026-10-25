package setup

import (
	"context"
	"crypto/rand"
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
	"time"
)

func StartLocalCluster(ctx context.Context, directory string, options LocalOptions) (*LocalCluster, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
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
		if json.Unmarshal(raw, &settings) != nil || settings.Socket != "127.0.0.1" || settings.Port < 1 || settings.Port > 65535 || settings.TCPRole != options.TCPRole {
			return nil, errors.New("local PostgreSQL configuration is invalid; Windows and Linux require separate state directories")
		}
	case errors.Is(err, os.ErrNotExist):
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		settings = localSettings{Socket: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, TCPRole: options.TCPRole}
		listener.Close()
		raw, _ = json.Marshal(settings)
		if err := os.WriteFile(settingsPath, raw, 0600); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	data := filepath.Join(directory, "postgres")
	password, err := windowsPostgresPassword(directory, data)
	if err != nil {
		return nil, err
	}
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
		command := exec.CommandContext(ctx, filepath.Join(binaries, "initdb.exe"), "-D", staging, "-U", "schakker", "--auth-local=scram-sha-256", "--auth-host=scram-sha-256", "--pwfile="+filepath.Join(directory, "postgres.key"), "--no-locale", "--encoding=UTF8")
		output, err := command.CombinedOutput()
		log := filepath.Join(directory, "postgres-init.log")
		if writeErr := os.WriteFile(log, output, 0600); writeErr != nil {
			return nil, writeErr
		}
		if err != nil {
			return nil, fmt.Errorf("local PostgreSQL initialization failed; inspect %s", log)
		}
		if err := os.Rename(staging, data); err != nil {
			return nil, err
		}
	}
	cluster := &LocalCluster{Directory: directory, Socket: settings.Socket, Port: settings.Port, password: password, control: filepath.Join(binaries, "pg_ctl.exe")}
	if exec.CommandContext(ctx, cluster.control, "-D", data, "status").Run() == nil {
		return cluster, nil
	}
	config, err := os.OpenFile(filepath.Join(data, "postgresql.conf"), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := fmt.Fprintf(config, "\nlisten_addresses = '127.0.0.1'\nunix_socket_directories = ''\nport = %d\n", settings.Port)
	closeErr := config.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	hba := "host all schakker 127.0.0.1/32 scram-sha-256\n"
	if options.TCPRole != "" && options.TCPRole != "schakker" {
		hba += "host all " + options.TCPRole + " 127.0.0.1/32 scram-sha-256\n"
	}
	hba += "host all all 0.0.0.0/0 reject\nhost all all ::0/0 reject\n"
	if err := os.WriteFile(filepath.Join(data, "pg_hba.conf"), []byte(hba), 0600); err != nil {
		return nil, err
	}
	if _, err := exec.CommandContext(ctx, cluster.control, "-D", data, "-l", filepath.Join(directory, "postgres.log"), "-w", "-t", "15", "start").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("local PostgreSQL did not start; inspect %s", filepath.Join(directory, "postgres.log"))
	}
	cluster.started = true
	return cluster, nil
}

func localDSN(c *LocalCluster, database string) string {
	query := url.Values{"sslmode": {"disable"}, "connect_timeout": {"5"}}
	return (&url.URL{Scheme: "postgres", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Port)), User: url.UserPassword("schakker", c.password), Path: "/" + database, RawQuery: query.Encode()}).String()
}

func windowsPostgresPassword(directory, data string) (string, error) {
	filename := filepath.Join(directory, "postgres.key")
	raw, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(data); !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("local PostgreSQL credentials are missing; restore postgres.key")
		}
		password := make([]byte, 32)
		if _, err := rand.Read(password); err != nil {
			return "", err
		}
		raw = []byte(hex.EncodeToString(password) + "\n")
		if err := os.WriteFile(filename, raw, 0600); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("local PostgreSQL credentials are invalid")
	}
	return value, nil
}

func postgresBinaries(ctx context.Context) (string, error) {
	directories := []string{}
	if configured := os.Getenv("SCHAKKER_POSTGRES_BIN"); configured != "" {
		directories = append(directories, configured)
	}
	if initdb, err := exec.LookPath("initdb.exe"); err == nil {
		directories = append(directories, filepath.Dir(initdb))
	}
	if config, err := exec.LookPath("pg_config.exe"); err == nil {
		lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
		output, err := exec.CommandContext(lookup, config, "--bindir").Output()
		cancel()
		if err == nil {
			directories = append(directories, strings.TrimSpace(string(output)))
		}
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		installed, _ := filepath.Glob(filepath.Join(root, "PostgreSQL", "*", "bin"))
		sort.Sort(sort.Reverse(sort.StringSlice(installed)))
		directories = append(directories, installed...)
	}
	for _, directory := range directories {
		valid := true
		for _, binary := range []string{"initdb.exe", "pg_ctl.exe", "postgres.exe"} {
			info, err := os.Stat(filepath.Join(directory, binary))
			if err != nil || info.IsDir() {
				valid = false
				break
			}
		}
		if valid {
			return directory, nil
		}
	}
	return "", errors.New("install PostgreSQL server tools for Windows, or set SCHAKKER_POSTGRES_BIN to their bin directory; an existing database can also be used with SCHAKKER_DATABASE_URL")
}
