package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/owlcms/obsreplays/internal/logging"
)

// Config represents the configuration file structure
type Config struct {
	Port     int    `toml:"port"`
	VideoDir string `toml:"videoDir"`
	OwlCMS   string `toml:"owlcms"`
	Platform string `toml:"platform"`
}

var (
	Verbose       bool
	NoVideo       bool
	InstallDir    string
	videoDir      string
	Recode        bool
	currentConfig *Config
)

// LoadConfig loads the configuration from the specified file
func LoadConfig(configFile string) (*Config, error) {
	// Ensure InstallDir is initialized
	if InstallDir == "" {
		InstallDir = GetInstallDir()
	}

	// Extract default config if no config file exists
	if err := ExtractDefaultConfig(configFile); err != nil {
		return nil, fmt.Errorf("failed to extract default config: %w", err)
	}

	var config Config

	if _, err := toml.DecodeFile(configFile, &config); err != nil {
		return nil, err
	}

	// Ensure VideoDir is absolute and default to "videos" if not specified
	if config.VideoDir == "" {
		config.VideoDir = "videos"
	}
	if !filepath.IsAbs(config.VideoDir) {
		config.VideoDir = filepath.Join(GetInstallDir(), config.VideoDir)
	}

	// Create VideoDir if it doesn't exist
	if err := os.MkdirAll(config.VideoDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create video directory: %w", err)
	}

	// Log the video directory
	logging.InfoLogger.Printf("Videos will be stored in: %s", config.VideoDir)

	// Set remaining recording package configurations
	SetVideoDir(config.VideoDir)

	// Log all configuration parameters
	platformKey := getPlatformName()
	logging.InfoLogger.Printf("Configuration loaded from %s for platform %s:\n"+
		"    Port: %d\n"+
		"    VideoDir: %s\n",
		configFile,
		platformKey,
		config.Port,
		config.VideoDir)

	// Store the current config for later use
	currentConfig = &config

	return &config, nil
}

// GetCurrentConfig returns the current configuration
func GetCurrentConfig() *Config {
	return currentConfig
}

// InitConfig processes command-line flags and loads the configuration
func InitConfig() (*Config, error) {
	configFile := flag.String("config", filepath.Join(GetInstallDir(), "config.toml"), "path to configuration file")
	flag.StringVar(&InstallDir, "dir", "obsreplays", fmt.Sprintf(
		`Name of an alternate installation directory. Default is 'obsreplays'.
Value is relative to the platform-specific directory for applcation data (%s)
Used for multiple installations on the same machine (e.g. 'replays2, replay3').
An absolute path can be provded if needed.`, GetInstallDir()))
	verbose := flag.Bool("v", false, "enable verbose logging")
	verboseAlt := flag.Bool("verbose", false, "enable verbose logging")
	flag.BoolVar(&NoVideo, "noVideo", false, "log ffmpeg actions but do not execute them")
	flag.Parse()

	// Set verbose mode in logging package
	logging.SetVerbose(*verbose || *verboseAlt)

	// Ensure logging directory is absolute
	logDir := filepath.Join(GetInstallDir(), "logs")

	// Initialize loggers
	if err := logging.Init(logDir); err != nil {
		return nil, fmt.Errorf("failed to initialize logging: %w", err)
	}

	// Load configuration
	cfg, err := LoadConfig(*configFile)
	if err != nil {
		return nil, fmt.Errorf("error loading configuration: %w", err)
	}

	return cfg, nil
}

// getInstallDir returns the installation directory based on the environment
func GetInstallDir() string {
	if InstallDir != "" && filepath.IsAbs(InstallDir) {
		return InstallDir
	}

	var baseDir string
	appName := "obsreplays"
	if InstallDir != "" {
		appName = InstallDir
	}

	switch runtime.GOOS {
	case "windows":
		baseDir = filepath.Join(os.Getenv("APPDATA"), appName)
	case "darwin":
		baseDir = filepath.Join(os.Getenv("HOME"), "Library", "Application Support", appName)
	case "linux":
		baseDir = filepath.Join(os.Getenv("HOME"), ".local", "share", appName)
	default:
		baseDir = "./" + appName
	}

	return baseDir
}

// isWSL checks if we're running under Windows Subsystem for Linux
func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft") ||
		strings.Contains(strings.ToLower(string(data)), "wsl")
}

// getPlatformName returns a string describing the current platform
func getPlatformName() string {
	if isWSL() {
		return "WSL"
	}
	return runtime.GOOS
}

func UpdateConfigFile(configFile, owlcmsAddress string) error {
	content, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	foundOwlcms := false
	portLineIndex := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# owlcms =") ||
			strings.HasPrefix(trimmed, "owlcms =") ||
			trimmed == "# owlcms" {
			address := owlcmsAddress
			if strings.Contains(address, ":") {
				address = strings.Split(address, ":")[0]
			}
			leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = fmt.Sprintf("%sowlcms = \"%s\"", leadingSpace, address)
			foundOwlcms = true
			break
		}
		if strings.HasPrefix(trimmed, "port") {
			portLineIndex = i
		}
	}

	if !foundOwlcms && portLineIndex >= 0 {
		address := owlcmsAddress
		if strings.Contains(address, ":") {
			address = strings.Split(address, ":")[0]
		}
		leadingSpace := lines[portLineIndex][:len(lines[portLineIndex])-len(strings.TrimLeft(lines[portLineIndex], " \t"))]
		newLine := fmt.Sprintf("%sowlcms = \"%s\"", leadingSpace, address)
		lines = append(lines[:portLineIndex+1], append([]string{newLine}, lines[portLineIndex+1:]...)...)
	}

	return os.WriteFile(configFile, []byte(strings.Join(lines, "\n")), 0644)
}

func UpdatePlatform(configFile, platform string) error {
	input, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	lines := strings.Split(string(input), "\n")
	platformFound := false

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "platform") {
			lines[i] = fmt.Sprintf("platform = \"%s\"", platform)
			platformFound = true
			break
		}
	}

	if !platformFound {
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "owlcms") {
				newLines := make([]string, 0, len(lines)+1)
				newLines = append(newLines, lines[:i+1]...)
				newLines = append(newLines, fmt.Sprintf("platform = \"%s\"", platform))
				newLines = append(newLines, lines[i+1:]...)
				lines = newLines
				break
			}
		}
	}

	output := strings.Join(lines, "\n")
	if err := os.WriteFile(configFile, []byte(output), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// SetVideoDir sets the video directory
func SetVideoDir(dir string) {
	videoDir = dir
}

// SetNoVideo sets the no video flag
func SetNoVideo(noVideo bool) {
	NoVideo = noVideo
}

// SetRecode sets the recode flag
func SetRecode(recode bool) {
	Recode = recode
}

// GetVideoDir returns the video directory
func GetVideoDir() string {
	return videoDir
}
