package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-gost/x/identity"
)

func directFallbackURL(rawURL string) string {
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd <= 0 {
		return ""
	}
	rest := rawURL[schemeEnd+3:]
	for _, prefix := range []string{"https://", "http://"} {
		if idx := strings.Index(rest, prefix); idx > 0 {
			return rest[idx:]
		}
	}
	return ""
}

func httpGetWithFallback(rawURL string) (*http.Response, error) {
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(rawURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	direct := directFallbackURL(rawURL)
	if direct == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	resp2, err2 := client.Get(direct)
	if err2 != nil {
		return nil, err2
	}
	if resp2.StatusCode != http.StatusOK {
		_ = resp2.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp2.StatusCode)
	}
	return resp2, nil
}

func (r *liteReporter) handleUpgradeAgent(raw json.RawMessage) error {
	defer debug.FreeOSMemory()

	var req struct {
		DownloadURL string `json:"downloadUrl"`
		ChecksumURL string `json:"checksumUrl"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("解析升级参数失败: %w", err)
	}
	if strings.TrimSpace(req.DownloadURL) == "" {
		return fmt.Errorf("下载地址不能为空")
	}

	downloadURL := strings.ReplaceAll(req.DownloadURL, "{ARCH}", runtime.GOARCH)
	checksumURL := strings.ReplaceAll(req.ChecksumURL, "{ARCH}", runtime.GOARCH)

	binaryPath := identity.BinaryPath()
	tmpPath := binaryPath + ".new"
	backupPath := binaryPath + ".old"

	r.sendUpgradeProgress("downloading", 0, "开始下载轻量升级包...")
	resp, err := httpGetWithFallback(downloadURL)
	if err != nil {
		return fmt.Errorf("下载升级包失败: %w", err)
	}
	defer resp.Body.Close()

	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(outFile, hasher), io.LimitReader(resp.Body, 128<<20))
	closeErr := outFile.Close()
	if err != nil || closeErr != nil || n == 0 {
		_ = os.Remove(tmpPath)
		if err != nil {
			return fmt.Errorf("写入升级包失败: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("关闭升级包失败: %w", closeErr)
		}
		return fmt.Errorf("下载的升级包为空")
	}
	r.sendUpgradeProgress("downloading", 100, fmt.Sprintf("下载完成 (%d bytes)", n))

	if checksumURL != "" {
		r.sendUpgradeProgress("verifying", 0, "校验文件完整性...")
		if cResp, err := httpGetWithFallback(checksumURL); err == nil {
			checksumBody, rErr := io.ReadAll(io.LimitReader(cResp.Body, 4096))
			_ = cResp.Body.Close()
			if rErr == nil {
				expected := strings.TrimSpace(strings.Split(string(checksumBody), " ")[0])
				actual := hex.EncodeToString(hasher.Sum(nil))
				if expected != "" && !strings.EqualFold(expected, actual) {
					_ = os.Remove(tmpPath)
					return fmt.Errorf("校验失败: 期望 %s, 实际 %s", expected, actual)
				}
			}
		}
		r.sendUpgradeProgress("verifying", 100, "校验通过")
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("设置执行权限失败: %w", err)
	}
	if err := copyFileSimple(binaryPath, backupPath, 0o755); err == nil {
		fmt.Println("📦 旧版本已备份到", backupPath)
	}

	r.sendUpgradeProgress("installing", 80, "准备重启...")
	if identity.Env("FLYX_AGENT_CONTAINER", "FLUX_AGENT_CONTAINER") == "1" {
		if err := os.Rename(tmpPath, binaryPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("替换程序失败: %w", err)
		}
		go func() {
			time.Sleep(time.Second)
			terminateSelf()
		}()
		r.sendUpgradeProgress("installing", 100, "重启中...")
		return nil
	}

	script := fmt.Sprintf(
		"sleep 1 && %s && mv %s %s && %s",
		agentServiceCommand(binaryPath, "stop"),
		shellQuote(tmpPath),
		shellQuote(binaryPath),
		agentServiceCommand(binaryPath, "start"),
	)
	if err := startDetachedCommand(script); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("启动重启脚本失败: %w", err)
	}
	r.sendUpgradeProgress("installing", 100, "重启中...")
	return nil
}

func (r *liteReporter) handleRollbackAgent() error {
	binaryPath := identity.BinaryPath()
	backupPath := binaryPath + ".old"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("没有可用的备份文件，无法回退")
	}
	if identity.Env("FLYX_AGENT_CONTAINER", "FLUX_AGENT_CONTAINER") == "1" {
		tmpPath := binaryPath + ".new"
		if err := copyFileSimple(backupPath, tmpPath, 0o755); err != nil {
			return fmt.Errorf("写入备份失败: %w", err)
		}
		if err := os.Rename(tmpPath, binaryPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("替换程序失败: %w", err)
		}
		go func() {
			time.Sleep(time.Second)
			terminateSelf()
		}()
		return nil
	}
	script := fmt.Sprintf(
		"sleep 1 && %s && cp %s %s && %s",
		agentServiceCommand(binaryPath, "stop"),
		shellQuote(backupPath),
		shellQuote(binaryPath),
		agentServiceCommand(binaryPath, "start"),
	)
	if err := startDetachedCommand(script); err != nil {
		return fmt.Errorf("启动回退脚本失败: %w", err)
	}
	return nil
}

func copyFileSimple(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func agentServiceCommand(binaryPath, action string) string {
	name := identity.AgentName
	if base := filepath.Base(binaryPath); base == "flux_agent" || base == identity.LiteAgentName {
		name = base
	}
	return "(if command -v rc-service >/dev/null 2>&1 && [ -f /etc/alpine-release ]; then rc-service " + name + " " + action + "; else systemctl " + action + " " + name + "; fi)"
}

func startDetachedCommand(script string) error {
	if _, err := os.Stat("/etc/alpine-release"); err == nil {
		return exec.Command("sh", "-c", "nohup sh -c "+shellQuote(script)+" </dev/null >/dev/null 2>&1 &").Start()
	}
	return exec.Command("systemd-run", "--quiet", "/bin/sh", "-c", script).Start()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
