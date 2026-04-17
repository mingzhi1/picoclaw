package cfgcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/mingzhi1/metaclaw/cmd/metaclaw/internal"
)

// --- list ---

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "l"},
		Short:   "List all configuration values",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runList()
		},
	}
}

func runList() error {
	data, err := readConfigRaw()
	if err != nil {
		return err
	}

	result := gjson.ParseBytes(data)
	result.ForEach(func(key, value gjson.Result) bool {
		printFlat("", key.String(), value)
		return true
	})
	return nil
}

// printFlat recursively prints nested JSON as dot-separated key=value.
func printFlat(prefix, key string, value gjson.Result) {
	path := key
	if prefix != "" {
		path = prefix + "." + key
	}

	if value.IsObject() {
		value.ForEach(func(k, v gjson.Result) bool {
			printFlat(path, k.String(), v)
			return true
		})
		return
	}

	if value.IsArray() {
		arr := value.Array()
		if len(arr) == 0 {
			return // skip empty arrays
		}
		// For model_list, show compact summary
		if strings.HasSuffix(path, "model_list") {
			for i, item := range arr {
				name := item.Get("model_name").String()
				model := item.Get("model").String()
				hasKey := item.Get("api_key").String() != ""
				keyStatus := "no-key"
				if hasKey {
					keyStatus = "has-key"
				}
				if item.Get("auth_method").String() != "" {
					keyStatus = item.Get("auth_method").String()
				}
				fmt.Printf("%s[%d].model_name=%s\n", path, i, name)
				fmt.Printf("%s[%d].model=%s\n", path, i, model)
				fmt.Printf("%s[%d].status=%s\n", path, i, keyStatus)
			}
			return
		}
		// Generic array
		for i, item := range arr {
			if item.IsObject() {
				item.ForEach(func(k, v gjson.Result) bool {
					printFlat(fmt.Sprintf("%s[%d]", path, i), k.String(), v)
					return true
				})
			} else {
				fmt.Printf("%s[%d]=%s\n", path, i, item.String())
			}
		}
		return
	}

	// Skip empty strings and false booleans to reduce noise
	s := value.String()
	if s == "" || s == "false" || s == "0" {
		return
	}

	// Mask sensitive values
	if isSensitive(key) && len(s) > 8 {
		s = s[:8] + "***"
	}

	fmt.Printf("%s=%s\n", path, s)
}

// --- get ---

func newGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value by dot-path",
		Args:  cobra.ExactArgs(1),
		Example: `  metaclaw config get agents.defaults.primary_model
  metaclaw config get gateway.port
  metaclaw config get channels.telegram.enabled`,
		RunE: func(_ *cobra.Command, args []string) error {
			return runGet(args[0])
		},
	}
}

func runGet(key string) error {
	data, err := readConfigRaw()
	if err != nil {
		return err
	}

	result := gjson.GetBytes(data, key)
	if !result.Exists() {
		return fmt.Errorf("key %q not found", key)
	}

	if result.IsObject() || result.IsArray() {
		fmt.Println(result.Raw)
	} else {
		fmt.Println(result.String())
	}
	return nil
}

// --- set ---

// knownTools maps tool short names to their detection config.
var knownTools = map[string]struct {
	cmds []string // candidate binary names (tried in order)
	args []string // version flag
}{
	"python": {[]string{"python3", "python"}, []string{"--version"}},
	"uv":     {[]string{"uv"}, []string{"--version"}},
	"npm":    {[]string{"npm"}, []string{"--version"}},
	"node":   {[]string{"node"}, []string{"--version"}},
	"git":    {[]string{"git"}, []string{"--version"}},
	"curl":   {[]string{"curl"}, []string{"--version"}},
}

func newSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> [value]",
		Short: "Set a config value, or auto-detect a tool path",
		Args:  cobra.RangeArgs(1, 2),
		Example: `  metaclaw config set python               # auto-detect python path
	metaclaw config set uv                   # auto-detect uv path
	metaclaw config set npm                  # auto-detect npm path
	metaclaw config set agents.defaults.primary_model deepseek-chat
	metaclaw config set gateway.port 8080`,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 {
				// Check if it's a known tool name
				if _, ok := knownTools[args[0]]; ok {
					return runSetTool(args[0])
				}
				return fmt.Errorf("unknown tool %q — known tools: python, uv, npm, node, git, curl\n"+
					"For key=value: metaclaw config set <key> <value>", args[0])
			}
			return runSet(args[0], args[1])
		},
	}
}

// runSetTool auto-detects a tool binary and saves its path to config.
func runSetTool(name string) error {
	tool, ok := knownTools[name]
	if !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}

	// Try each candidate command
	for _, cmd := range tool.cmds {
		path, err := exec.LookPath(cmd)
		if err != nil {
			continue
		}

		// Get version
		out, err := exec.Command(path, tool.args...).Output()
		ver := "unknown"
		if err == nil {
			ver = strings.TrimSpace(strings.Split(string(out), "\n")[0])
		}

		// Save to config as tools.env.<name>
		configKey := "tools.env." + name
		if err := runSet(configKey, path); err != nil {
			return err
		}

		fmt.Printf("✅ %s: %s (%s)\n", name, path, ver)
		return nil
	}

	fmt.Printf("❌ %s: not found in PATH\n", name)
	fmt.Printf("   Install %s and try again, or set manually:\n", name)
	fmt.Printf("   metaclaw config set tools.env.%s /path/to/%s\n", name, name)
	return nil
}

func runSet(key, value string) error {
	configPath := internal.GetConfigPath()
	data, err := readConfigRaw()
	if err != nil {
		// Config doesn't exist yet — create minimal one
		if os.IsNotExist(err) {
			data = []byte("{}")
		} else {
			return err
		}
	}

	// Auto-detect value type
	var typedValue interface{} = value
	if value == "true" {
		typedValue = true
	} else if value == "false" {
		typedValue = false
	} else if n, err := strconv.Atoi(value); err == nil {
		typedValue = n
	}

	newData, err := sjson.SetBytes(data, key, typedValue)
	if err != nil {
		return fmt.Errorf("failed to set %q: %w", key, err)
	}

	// Pretty-print before saving
	var pretty json.RawMessage = newData
	formatted, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		formatted = newData
	}

	if err := os.WriteFile(configPath, formatted, 0o600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("%s = %v\n", key, typedValue)
	return nil
}

// --- path ---

func newPathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Show config file path",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(internal.GetConfigPath())
		},
	}
}

// --- helpers ---

func readConfigRaw() ([]byte, error) {
	return os.ReadFile(internal.GetConfigPath())
}

func isSensitive(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "key") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password")
}
