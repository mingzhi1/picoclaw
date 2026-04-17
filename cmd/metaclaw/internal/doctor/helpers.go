package doctor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal"
	"github.com/mingzhi1/metaclaw/pkg/infra/config"
)

// symbols for terminal output
const (
	pass = "✅"
	fail = "❌"
	warn = "⚠️"
	skip = "⬚"
)

func runDoctor() error {
	fmt.Printf("%s metaclaw doctor\n", internal.Logo)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	checkRuntime()
	checkToolchain()
	cfg := checkConfig()
	if cfg == nil {
		fmt.Println()
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		return nil // can't continue without config
	}
	checkModels(cfg)
	checkChannels(cfg)
	checkGatewayPort(cfg)
	checkWorkspace(cfg)
	checkEnvVars()

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	return nil
}

// --- individual checks ---

func checkRuntime() {
	fmt.Printf("%s Runtime: %s/%s, Go %s\n",
		pass, runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Printf("%s Version: %s\n", pass, internal.FormatVersion())
}

// checkToolchain probes for common tools that MetaClaw skills/tools may depend on.
func checkToolchain() {
	tools := []struct {
		name    string
		cmd     string
		args    []string
		require bool // true = error if missing, false = warn/skip
	}{
		{"git", "git", []string{"--version"}, false},
		{"node", "node", []string{"--version"}, false},
		{"npm", "npm", []string{"--version"}, false},
		{"python", pythonCmd(), []string{"--version"}, false},
		{"uv", "uv", []string{"--version"}, false},
		{"curl", "curl", []string{"--version"}, false},
	}

	found := []string{}
	missing := []string{}

	for _, t := range tools {
		if t.cmd == "" {
			missing = append(missing, t.name)
			continue
		}
		out, err := exec.Command(t.cmd, t.args...).Output()
		if err != nil {
			missing = append(missing, t.name)
		} else {
			ver := strings.TrimSpace(strings.Split(string(out), "\n")[0])
			found = append(found, fmt.Sprintf("%s(%s)", t.name, ver))
		}
	}

	if len(found) > 0 {
		fmt.Printf("%s Tools: %s\n", pass, strings.Join(found, ", "))
	}
	if len(missing) > 0 {
		fmt.Printf("%s Tools not found: %s\n", skip, strings.Join(missing, ", "))
	}
}

// pythonCmd returns the python command name for the current OS.
func pythonCmd() string {
	// Windows: python, Unix: python3
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

func checkConfig() *config.Config {
	configPath := internal.GetConfigPath()

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("%s Config: %s not found\n", fail, configPath)
		fmt.Println("   → Run: metaclaw init")
		return nil
	}

	cfg, err := internal.LoadConfig()
	if err != nil {
		fmt.Printf("%s Config: %s — parse error: %v\n", fail, configPath, err)
		fmt.Println("   → Fix JSON syntax in config file")
		return nil
	}

	fmt.Printf("%s Config: %s\n", pass, configPath)
	return cfg
}

func checkModels(cfg *config.Config) {
	// skeleton — filled next
	if len(cfg.ModelList) == 0 {
		checkModelsEmpty(cfg)
		return
	}
	checkModelsPresent(cfg)
}

func checkModelsEmpty(cfg *config.Config) {
	if cfg.HasProvidersConfig() {
		fmt.Printf("%s Models: no model_list, using deprecated 'providers'\n", warn)
		fmt.Println("   → Migrate to model_list format")
	} else {
		fmt.Printf("%s Models: no models configured\n", fail)
		fmt.Println("   → Add a model: metaclaw chat \"配置 deepseek\"")
	}
}

func checkModelsPresent(cfg *config.Config) {
	primary := cfg.Agents.Defaults.GetPrimaryModel()
	hasKey := false
	primaryFound := false

	for _, m := range cfg.ModelList {
		if m.APIKey != "" || m.AuthMethod != "" {
			hasKey = true
		}
		if m.ModelName == primary {
			primaryFound = true
		}
	}

	fmt.Printf("%s Models: %d configured\n", pass, len(cfg.ModelList))

	if !hasKey {
		fmt.Printf("%s API Key: no model has an api_key set\n", fail)
		fmt.Println("   → Set PC_API_KEY=sk-xxx or add api_key to model_list")
	} else {
		fmt.Printf("%s API Key: at least one model has credentials\n", pass)
	}

	if primary == "" {
		fmt.Printf("%s Primary model: not set\n", warn)
		fmt.Println("   → Set agents.defaults.primary_model or PC_MODEL=xxx")
	} else if !primaryFound {
		fmt.Printf("%s Primary model: \"%s\" not found in model_list\n", fail, primary)
		available := modelNames(cfg)
		fmt.Printf("   → Available: %s\n", strings.Join(available, ", "))
	} else {
		fmt.Printf("%s Primary model: %s\n", pass, primary)
	}
}

func checkChannels(cfg *config.Config) {
	// skeleton — filled next
	enabledChannels := getEnabledChannels(cfg)
	if len(enabledChannels) == 0 {
		fmt.Printf("%s Channels: none enabled\n", skip)
		fmt.Println("   → Enable one: metaclaw chat \"配置 telegram\"")
		return
	}
	fmt.Printf("%s Channels: %s\n", pass, strings.Join(enabledChannels, ", "))
}

func checkGatewayPort(cfg *config.Config) {
	// skeleton — filled next
	host := cfg.Gateway.Host
	port := cfg.Gateway.Port
	addr := fmt.Sprintf("%s:%d", host, port)

	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		fmt.Printf("%s Gateway: %s (port available)\n", pass, addr)
	} else {
		conn.Close()
		fmt.Printf("%s Gateway: %s is already in use\n", warn, addr)
		fmt.Println("   → Another MetaClaw instance running? Or change gateway.port")
	}
}

func checkWorkspace(cfg *config.Config) {
	ws := cfg.WorkspacePath()
	if _, err := os.Stat(ws); os.IsNotExist(err) {
		fmt.Printf("%s Workspace: %s not found\n", warn, ws)
		fmt.Println("   → Run: metaclaw init")
		return
	}

	// Check bootstrap files
	missing := []string{}
	for _, f := range []string{"SOUL.md", "IDENTITY.md", "USER.md"} {
		path := filepath.Join(ws, f)
		info, err := os.Stat(path)
		if os.IsNotExist(err) || (err == nil && info.Size() < 50) {
			missing = append(missing, f)
		}
	}

	if len(missing) > 0 {
		fmt.Printf("%s Workspace: %s (customize: %s)\n", warn, ws, strings.Join(missing, ", "))
	} else {
		fmt.Printf("%s Workspace: %s\n", pass, ws)
	}
}

func checkEnvVars() {
	envChecks := []struct {
		short string
		full  string
	}{
		{"PC_MODEL", "PICOCLAW_AGENTS_DEFAULTS_PRIMARY_MODEL"},
		{"PC_API_KEY", ""},
		{"PC_PROXY", "PICOCLAW_PROXY"},
		{"PC_TG_TOKEN", "PICOCLAW_CHANNELS_TELEGRAM_TOKEN"},
		{"PC_CHANNEL", ""},
	}

	active := []string{}
	for _, e := range envChecks {
		if v := os.Getenv(e.short); v != "" {
			active = append(active, e.short+"="+maskValue(v))
		}
		if e.full != "" {
			if v := os.Getenv(e.full); v != "" {
				active = append(active, e.full+"="+maskValue(v))
			}
		}
	}

	if len(active) == 0 {
		fmt.Printf("%s Env vars: none set\n", skip)
	} else {
		fmt.Printf("%s Env vars: %s\n", pass, strings.Join(active, ", "))
	}
}

// --- helpers ---

func modelNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.ModelList))
	for _, m := range cfg.ModelList {
		names = append(names, m.ModelName)
	}
	return names
}

func maskValue(v string) string {
	if len(v) <= 8 {
		return v[:min(3, len(v))] + "***"
	}
	return v[:8] + "***"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getEnabledChannels(cfg *config.Config) []string {
	var enabled []string
	ch := cfg.Channels
	if ch.Telegram.Enabled {
		enabled = append(enabled, "telegram")
	}
	if ch.Feishu.Enabled {
		enabled = append(enabled, "feishu")
	}
	return enabled
}
