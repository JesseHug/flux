package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/go-gost/x/identity"
)

type agentConfig struct {
	Addr   string `json:"addr"`
	Secret string `json:"secret"`
}

func loadConfig(path string) (*agentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var cfg agentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("服务器地址不能为空")
	}
	if strings.TrimSpace(cfg.Secret) == "" {
		return nil, fmt.Errorf("服务器密钥不能为空")
	}
	return &cfg, nil
}

func applyEnvConfig(path string) error {
	addr := identity.Env("FLUX_AGENT_ADDR", "FLYX_AGENT_ADDR")
	secret := identity.Env("FLUX_AGENT_SECRET", "FLYX_AGENT_SECRET")
	if addr == "" && secret == "" {
		return nil
	}
	var cfg agentConfig
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("解析配置文件失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	if addr != "" {
		cfg.Addr = addr
	}
	if secret != "" {
		cfg.Secret = secret
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
