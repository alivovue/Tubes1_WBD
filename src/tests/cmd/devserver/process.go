package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type child struct {
	name string
	cmd  *exec.Cmd
	log  *os.File
	done chan struct{}
}

func launch(state, root, name, binary, directory string, values map[string]string) (*child, error) {
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	log, err := os.OpenFile(filepath.Join(state, name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	command := exec.Command(filepath.Join(state, "bin", binary))
	command.Dir = filepath.Join(root, "src", directory)
	command.Stdout, command.Stderr = log, log
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, replaced := values[key]; !replaced {
			command.Env = append(command.Env, value)
		}
	}
	for key, value := range values {
		command.Env = append(command.Env, key+"="+value)
	}
	hideWindow(command)
	if err := command.Start(); err != nil {
		log.Close()
		return nil, err
	}
	process := &child{name: name, cmd: command, log: log, done: make(chan struct{})}
	go func() { command.Wait(); close(process.done) }()
	return process, nil
}
func (process *child) close() {
	select {
	case <-process.done:
	default:
		if err := process.cmd.Process.Signal(os.Interrupt); err != nil {
			process.cmd.Process.Kill()
		}
		select {
		case <-process.done:
		case <-time.After(8 * time.Second):
			process.cmd.Process.Kill()
			<-process.done
		}
	}
	process.log.Close()
}
func (process *child) waitReady(ctx context.Context, address string) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-process.done:
			return fmt.Errorf("%s did not start; inspect %s", process.name, process.log.Name())
		case <-deadline.C:
			return fmt.Errorf("%s startup timed out; inspect %s", process.name, process.log.Name())
		case <-ticker.C:
			response, err := client.Get(address)
			if err == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}
