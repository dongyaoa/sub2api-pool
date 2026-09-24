package poolupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const DefaultConfigPath = "/etc/sub2api-pool-updater/config.json"
const ContainerSocketDirectory = "/run/sub2api-pool-updater"

type Config struct {
	ComposeFiles         []string `json:"compose_files"`
	ProjectName          string   `json:"project_name"`
	WorkingDir           string   `json:"working_dir"`
	EnvFiles             []string `json:"env_files,omitempty"`
	SocketPath           string   `json:"socket_path"`
	SocketGID            int      `json:"socket_gid"`
	StateDir             string   `json:"state_dir"`
	DockerPath           string   `json:"docker_path"`
	ComposeCommand       []string `json:"compose_command"`
	HealthTimeoutSeconds int      `json:"health_timeout_seconds"`
}

func LoadConfig(path string) (Config, error) {
	cfg := Config{SocketPath: "/run/sub2api-pool-updater/updater.sock", SocketGID: 1000, StateDir: "/var/lib/sub2api-pool-updater", DockerPath: "/usr/bin/docker", HealthTimeoutSeconds: 180}
	if err := secureFile(path); err != nil {
		return cfg, fmt.Errorf("trusted updater config: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(io.LimitReader(f, 65537))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return cfg, errors.New("config must contain exactly one JSON object")
	}
	if len(cfg.ComposeCommand) == 0 {
		cfg.ComposeCommand = []string{cfg.DockerPath, "compose"}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	for _, p := range append(append([]string{cfg.DockerPath, cfg.ComposeCommand[0]}, cfg.ComposeFiles...), cfg.EnvFiles...) {
		if err := secureFile(p); err != nil {
			return cfg, fmt.Errorf("trusted deployment file %s: %w", p, err)
		}
	}
	if err := secureParents(cfg.WorkingDir); err != nil {
		return cfg, err
	}
	if err := secureParents(filepath.Dir(cfg.StateDir)); err != nil {
		return cfg, err
	}
	if err := secureParents(filepath.Dir(filepath.Dir(cfg.SocketPath))); err != nil {
		return cfg, err
	}
	for _, path := range []string{cfg.StateDir, filepath.Dir(cfg.SocketPath)} {
		if err := secureExistingDirectory(path); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`).MatchString(c.ProjectName) {
		return errors.New("invalid compose project_name")
	}
	if len(c.ComposeFiles) == 0 || len(c.ComposeFiles) > 16 {
		return errors.New("one to sixteen compose_files required")
	}
	if len(c.EnvFiles) > 16 || c.SocketGID < 0 || c.HealthTimeoutSeconds < 10 || c.HealthTimeoutSeconds > 900 {
		return errors.New("invalid updater limits")
	}
	if len(c.ComposeCommand) != 1 && len(c.ComposeCommand) != 2 {
		return errors.New("compose_command must be an absolute executable, optionally followed by compose")
	}
	if len(c.ComposeCommand) == 2 && (c.ComposeCommand[0] != c.DockerPath || c.ComposeCommand[1] != "compose") {
		return errors.New("only docker compose is accepted as a two-item compose_command")
	}
	for _, p := range append(append([]string{c.WorkingDir, c.StateDir, c.SocketPath, c.DockerPath, c.ComposeCommand[0]}, c.ComposeFiles...), c.EnvFiles...) {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n") {
			return errors.New("configuration paths must be clean absolute paths")
		}
	}
	if c.StateDir == string(filepath.Separator) || filepath.Dir(c.SocketPath) == string(filepath.Separator) {
		return errors.New("dedicated state and socket directories required")
	}
	return nil
}

func secureFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return errors.New("must be a regular file not writable by group or other users")
	}
	if err := checkRootOwner(info); err != nil {
		return err
	}
	return secureParents(filepath.Dir(path))
}

func secureParents(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return errors.New("trusted paths require non-writable directory parents without symlinks")
		}
		if err := checkRootOwner(info); err != nil {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func secureExistingDirectory(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return secureParents(filepath.Dir(path))
	} else if err != nil {
		return err
	}
	return secureParents(path)
}

func (c Config) validateDeploymentPaths() error {
	for _, path := range append(append([]string{c.DockerPath, c.ComposeCommand[0]}, c.ComposeFiles...), c.EnvFiles...) {
		if err := secureFile(path); err != nil {
			return err
		}
	}
	for _, path := range []string{c.WorkingDir, c.StateDir, filepath.Dir(c.SocketPath)} {
		if err := secureExistingDirectory(path); err != nil {
			return err
		}
	}
	return nil
}
