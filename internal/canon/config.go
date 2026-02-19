package canon

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type CanonConfig struct {
	AI AIConfig
}

type AIConfig struct {
	Provider string
}

func DefaultConfig() CanonConfig {
	return CanonConfig{
		AI: AIConfig{
			Provider: "codex",
		},
	}
}

func LoadConfig(root string) (CanonConfig, error) {
	cfg := DefaultConfig()

	homeDir, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(homeDir) != "" {
		globalPath := filepath.Join(homeDir, ".canonconfig")
		if err := applyConfigFile(&cfg, globalPath); err != nil {
			return cfg, err
		}
	}

	localPath := filepath.Join(root, ".canonconfig")
	if err := applyConfigFile(&cfg, localPath); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func applyConfigFile(cfg *CanonConfig, path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.ToLower(strings.TrimSpace(parts[1]))
		if section == "ai" {
			switch key {
			case "provider":
				if value == "codex" || value == "claude" {
					cfg.AI.Provider = value
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
