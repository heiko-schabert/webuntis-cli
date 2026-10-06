package untis

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Config holds the values shown in WebUntis under Profile > Freigaben >
// Zugriff über Untis Mobile.
type Config struct {
	Server, School, User, Secret string
	Student                      string // default child's first name
}

// DefaultEnvFile follows XDG ($XDG_CONFIG_HOME, else ~/.config) on every OS;
// os.UserConfigDir would pick ~/Library/Application Support on macOS.
func DefaultEnvFile() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "webuntis", "env")
}

// LoadConfig prefers env vars over the file so an MCP client config can override it.
func LoadConfig(getenv func(string) string, envFile string) (Config, error) {
	file := map[string]string{}
	b, err := os.ReadFile(envFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			file[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	get := func(k string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return file[k]
	}
	c := Config{
		Server:  get("WEBUNTIS_SERVER"),
		School:  get("WEBUNTIS_SCHOOL"),
		User:    get("WEBUNTIS_USERNAME"),
		Secret:  get("WEBUNTIS_SECRET"),
		Student: get("WEBUNTIS_STUDENT"),
	}
	var missing []string
	for k, v := range map[string]string{"WEBUNTIS_SERVER": c.Server, "WEBUNTIS_SCHOOL": c.School, "WEBUNTIS_USERNAME": c.User, "WEBUNTIS_SECRET": c.Secret} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return Config{}, fmt.Errorf("missing config: %s (environment or %s)", strings.Join(missing, ", "), envFile)
	}
	return c, nil
}
