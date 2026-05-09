package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadEnvFile loads simple KEY=value environment entries from path.
// Existing process environment variables are preserved.
func LoadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=value", path, lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s:%d: empty key", path, lineNo)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, parseEnvFileValue(strings.TrimSpace(value))); err != nil {
			return fmt.Errorf("%s:%d: set %s: %w", path, lineNo, key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func parseEnvFileValue(value string) string {
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

// DesktopConfigFilePath returns the desktop app config file path that can be
// edited outside the app when the packaged desktop process does not inherit a
// shell environment.
func DesktopConfigFilePath() string {
	return desktopConfigFilePath()
}

func desktopConfigFilePath() string {
	if override := strings.TrimSpace(os.Getenv("OPENVIBELY_DESKTOP_CONFIG_FILE")); override != "" {
		return override
	}
	return filepath.Join(desktopDataDir(), "config.env")
}
