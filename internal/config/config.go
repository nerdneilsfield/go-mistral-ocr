package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config 应用程序配置
type Config struct {
	// API配置
	APIKeys  []string `mapstructure:"api_keys"`
	BaseURLs []string `mapstructure:"base_urls"`

	// 错误处理配置
	ContinueOnError        bool `mapstructure:"continue_on_error"`
	RetryDifferentEndpoint bool `mapstructure:"retry_different_endpoint"`

	// 输出配置
	OutputDir           string `mapstructure:"output_dir"`
	IncludeImages       bool   `mapstructure:"include_images"`
	DefaultOutputFormat string `mapstructure:"default_output_format"`

	// 日志配置
	LogLevel  string `mapstructure:"log_level"`
	LogFile   string `mapstructure:"log_file"`
	LogFormat string `mapstructure:"log_format"`

	// GUI配置
	Theme string `mapstructure:"theme"`
}

// LoadConfig 从viper加载配置
func LoadConfig() (*Config, error) {
	// 设置默认值
	setDefaults()

	// 尝试从配置文件加载
	if err := loadConfigFile(); err != nil {
		// 如果找不到配置文件，创建一个默认配置
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			if err := createDefaultConfig(); err != nil {
				return nil, fmt.Errorf("无法创建默认配置: %w", err)
			}
		} else {
			return nil, fmt.Errorf("加载配置文件出错: %w", err)
		}
	}

	// 从环境变量加载配置
	loadFromEnv()

	// 解析配置到结构体
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("解析配置出错: %w", err)
	}

	// 兼容旧版配置：如果 api_key 存在但 api_keys 不存在，则将 api_key 添加到 api_keys
	if apiKey := viper.GetString("api_key"); apiKey != "" && len(config.APIKeys) == 0 {
		config.APIKeys = append(config.APIKeys, apiKey)
	}

	// 兼容旧版配置：如果 base_url 存在但 base_urls 不存在，则将 base_url 添加到 base_urls
	if baseURL := viper.GetString("base_url"); baseURL != "" && len(config.BaseURLs) == 0 {
		config.BaseURLs = append(config.BaseURLs, baseURL)
	}

	// 验证配置
	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// setDefaults 设置默认配置
func setDefaults() {
	viper.SetDefault("base_url", "https://api.mistral.ai/v1/")
	viper.SetDefault("output_dir", "./output")
	viper.SetDefault("include_images", true)
	viper.SetDefault("default_output_format", "markdown")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "console")
	viper.SetDefault("theme", "light")
	viper.SetDefault("continue_on_error", true)
	viper.SetDefault("retry_different_endpoint", true)
}

// loadConfigFile 尝试加载配置文件
func loadConfigFile() error {
	// 设置配置文件名称
	viper.SetConfigName("config")
	viper.SetConfigType("toml")

	// 添加配置文件路径
	// 1. 当前工作目录
	viper.AddConfigPath(".")

	// 2. 用户配置目录
	homeDir, err := os.UserHomeDir()
	if err == nil {
		viper.AddConfigPath(filepath.Join(homeDir, ".config", "mistral-ocr"))
	}

	// 3. 系统配置目录
	viper.AddConfigPath("/etc/mistral-ocr")

	// 加载配置文件
	return viper.ReadInConfig()
}

// createDefaultConfig 创建默认配置文件
func createDefaultConfig() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(homeDir, ".config", "mistral-ocr")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.toml")

	// 默认配置内容
	defaultConfig := `# Mistral OCR 配置文件

# API配置
# 支持多个API密钥轮询，程序会在每次API调用时随机选择一个密钥开始，然后轮流使用
# 这有助于负载均衡和提高可靠性，当一个API密钥达到速率限制时可以自动切换到下一个
api_keys = [""]  # 在这里设置你的API密钥，或者使用MISTRAL_API_KEY环境变量，支持多个API密钥轮询

# 支持多个API基础URL轮询，程序会在每次API调用时随机选择一个URL开始，然后轮流使用
# 这有助于在某个API端点不可用时自动切换到备用端点
base_urls = ["https://api.mistral.ai/v1/"]  # 可以添加多个备用API端点

# 错误处理配置
continue_on_error = true  # 当处理多个文件时，如果一个文件处理失败，是否继续处理其他文件
retry_different_endpoint = true  # 当一个API端点失败时，是否尝试使用不同的端点重试

# 输出配置
output_dir = "./output"
include_images = true
default_output_format = "markdown"  # markdown 或 text

# 日志配置
log_level = "info"  # debug, info, warn, error
log_file = ""      # 留空表示输出到控制台
log_format = "console"  # console 或 json

# GUI配置
theme = "light"  # light 或 dark
`

	// 写入默认配置文件
	return os.WriteFile(configPath, []byte(defaultConfig), 0644)
}

// loadFromEnv 从环境变量加载配置
func loadFromEnv() {
	// 设置环境变量前缀
	viper.SetEnvPrefix("MISTRAL")

	// 将MISTRAL_API_KEY映射到api_key
	viper.BindEnv("api_key", "MISTRAL_API_KEY")

	// 自动映射其他环境变量
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

// validateConfig 验证配置
func validateConfig(config *Config) error {
	// 如果API密钥为空，查找环境变量
	if len(config.APIKeys) == 0 {
		apiKey := os.Getenv("MISTRAL_API_KEY")
		if apiKey != "" {
			config.APIKeys = append(config.APIKeys, apiKey)
		}
	}

	// 确保至少有一个 API 密钥
	if len(config.APIKeys) == 0 {
		return fmt.Errorf("至少需要一个 API 密钥")
	}

	// 确保至少有一个 BaseURL
	if len(config.BaseURLs) == 0 {
		config.BaseURLs = append(config.BaseURLs, "https://api.mistral.ai/v1/")
	}

	// 确保每个 BaseURL 都以 / 结尾
	for i, baseURL := range config.BaseURLs {
		if baseURL != "" && !strings.HasSuffix(baseURL, "/") {
			config.BaseURLs[i] = baseURL + "/"
		}
	}

	// 确保输出目录存在
	if config.OutputDir != "" {
		if _, err := os.Stat(config.OutputDir); os.IsNotExist(err) {
			if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
				return fmt.Errorf("无法创建输出目录: %w", err)
			}
		}
	}

	return nil
}

// UpdateConfig 更新配置
func UpdateConfig(key string, value interface{}) error {
	viper.Set(key, value)
	return viper.WriteConfig()
}

// SaveConfig 保存当前配置到文件
func SaveConfig(config *Config) error {
	for k, v := range map[string]interface{}{
		"api_keys":              config.APIKeys,
		"base_urls":             config.BaseURLs,
		"output_dir":            config.OutputDir,
		"include_images":        config.IncludeImages,
		"default_output_format": config.DefaultOutputFormat,
		"log_level":             config.LogLevel,
		"log_file":              config.LogFile,
		"log_format":            config.LogFormat,
		"theme":                 config.Theme,
	} {
		viper.Set(k, v)
	}

	return viper.WriteConfig()
}

// LoadConfigFromFile 从指定路径加载配置文件
func LoadConfigFromFile(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if err := validateConfig(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// GetDefaultConfig 返回默认配置文件内容
func GetDefaultConfig() string {
	return `# Mistral OCR 配置文件

# API配置
# 支持多个API密钥轮询，程序会在每次API调用时随机选择一个密钥开始，然后轮流使用
# 这有助于负载均衡和提高可靠性，当一个API密钥达到速率限制时可以自动切换到下一个
api_keys = [""]  # 在这里设置你的API密钥，或者使用MISTRAL_API_KEY环境变量，支持多个API密钥轮询

# 支持多个API基础URL轮询，程序会在每次API调用时随机选择一个URL开始，然后轮流使用
# 这有助于在某个API端点不可用时自动切换到备用端点
base_urls = ["https://api.mistral.ai/v1/"]  # 可以添加多个备用API端点

# 错误处理配置
continue_on_error = true  # 当处理多个文件时，如果一个文件处理失败，是否继续处理其他文件
retry_different_endpoint = true  # 当一个API端点失败时，是否尝试使用不同的端点重试

# 输出配置
output_dir = "./output"
include_images = true
default_output_format = "markdown"  # markdown 或 text

# 日志配置
log_level = "info"  # debug, info, warn, error
log_file = ""      # 留空表示输出到控制台
log_format = "console"  # console 或 json

# GUI配置
theme = "light"  # light 或 dark
`
}
