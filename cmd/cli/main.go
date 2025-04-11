package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/nerdneilsfield/go-mistral-ocr/internal/config"
	"github.com/nerdneilsfield/go-mistral-ocr/internal/logger"
	"github.com/nerdneilsfield/go-mistral-ocr/pkg/ocr"
)

var (
	// 默认配置
	cfg *config.Config
	log *zap.Logger

	// 命令行参数
	configFile    string
	apiKeys       []string
	baseURLs      []string
	outputDir     string
	includeImages bool
	outputName    string
	logLevel      string
	dryRun        bool
	timeout       int
	maxRetries    int
)

// 配置生成相关参数
var (
	outputToFile string
)

func main() {
	// 创建根命令
	rootCmd := &cobra.Command{
		Use:   "mistral-ocr",
		Short: "使用Mistral API进行OCR处理",
		Long:  `使用Mistral API识别PDF文件或URL中的文本，并将结果保存为Markdown和文本格式。`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// 跳过gen命令的配置加载
			if cmd.Name() == "gen" && cmd.Parent().Name() == "config" {
				return nil
			}
			return setup()
		},
	}

	// 处理文件命令
	processFileCmd := &cobra.Command{
		Use:   "file [文件路径或目录...]",
		Short: "处理本地PDF文件或目录",
		Long:  `处理一个或多个本地PDF文件，或者处理目录中的所有PDF文件。`,
		Args:  cobra.MinimumNArgs(1),
		RunE:  processFile,
	}

	// 处理URL命令
	processURLCmd := &cobra.Command{
		Use:   "url [URL]",
		Short: "直接处理URL指向的文件",
		Args:  cobra.ExactArgs(1),
		RunE:  processURL,
	}

	// 转换JSON命令
	convertCmd := &cobra.Command{
		Use:   "convert [JSON文件路径]",
		Short: "将JSON文件转换为Markdown文件",
		Long:  `将已有的OCR JSON响应文件转换为Markdown文件，无需重新调用API。`,
		Args:  cobra.ExactArgs(1),
		RunE:  convertJSON,
	}

	// 配置命令
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "管理配置",
	}

	// 设置API密钥命令
	setAPIKeyCmd := &cobra.Command{
		Use:   "set-api-key [API密钥]",
		Short: "设置Mistral API密钥",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.UpdateConfig("api_key", args[0]); err != nil {
				return err
			}
			fmt.Println("API密钥已更新")
			return nil
		},
	}

	// 生成默认配置命令
	genConfigCmd := &cobra.Command{
		Use:   "gen",
		Short: "生成默认配置",
		Long:  "生成默认配置并输出到标准输出或指定文件",
		RunE:  generateConfig,
	}

	// 添加根命令标志
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "指定配置文件路径")
	rootCmd.PersistentFlags().StringSliceVar(&apiKeys, "api-keys", nil, "Mistral API密钥列表，用逗号分隔")
	rootCmd.PersistentFlags().StringSliceVar(&baseURLs, "base-urls", nil, "Mistral API基础URL列表，用逗号分隔")
	rootCmd.PersistentFlags().StringVar(&outputDir, "output-dir", "", "输出目录")
	rootCmd.PersistentFlags().BoolVar(&includeImages, "include-images", true, "是否包含图片")
	rootCmd.PersistentFlags().StringVar(&outputName, "output-name", "", "输出文件名")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "日志级别 (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "不执行实际操作，仅打印将要执行的操作")
	rootCmd.PersistentFlags().IntVar(&timeout, "timeout", 10, "API请求超时时间（分钟）")
	rootCmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 3, "API请求最大重试次数")

	// 添加genConfig命令标志
	genConfigCmd.Flags().StringVarP(&outputToFile, "output", "o", "", "将配置输出到文件而非标准输出")

	// 添加子命令
	rootCmd.AddCommand(processFileCmd)
	rootCmd.AddCommand(processURLCmd)
	rootCmd.AddCommand(convertCmd)
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(setAPIKeyCmd)
	configCmd.AddCommand(genConfigCmd)

	// 执行命令
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// setup 初始化应用程序
func setup() error {
	var err error

	// 先初始化一个基本日志记录器，用于记录配置加载过程
	tempLogger, _ := zap.NewProduction()
	defer tempLogger.Sync()

	// 记录是否使用了自定义配置文件
	if configFile != "" {
		tempLogger.Info("使用自定义配置文件", zap.String("path", configFile))
	}

	// 加载配置，优先使用命令行指定的配置文件
	if configFile != "" {
		cfg, err = loadCustomConfig(configFile)
	} else {
		tempLogger.Debug("使用默认配置文件路径")
		cfg, err = config.LoadConfig()
	}

	if err != nil {
		tempLogger.Error("加载配置失败", zap.Error(err))
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 从命令行参数更新配置
	updateConfigFromFlags(tempLogger)

	// 初始化正式日志
	tempLogger.Debug("初始化日志系统", zap.String("level", cfg.LogLevel))
	log, err = logger.InitLogger(cfg.LogLevel, cfg.LogFormat, cfg.LogFile)
	if err != nil {
		tempLogger.Error("初始化日志系统失败", zap.Error(err))
		return fmt.Errorf("初始化日志失败: %w", err)
	}

	// 记录配置加载完成
	log.Info("配置加载完成",
		zap.Strings("baseURLs", cfg.BaseURLs),
		zap.String("outputDir", cfg.OutputDir),
		zap.Bool("includeImages", cfg.IncludeImages),
		zap.String("logLevel", cfg.LogLevel))

	// 检查API密钥是否存在
	// 对于convert命令，不需要API密钥
	cmd := os.Args[1]
	if cmd != "convert" && cmd != "help" && cmd != "version" && (len(cfg.APIKeys) == 0 || cfg.APIKeys[0] == "") {
		log.Error("缺少API密钥")
		return fmt.Errorf("缺少API密钥，请使用 --api-keys 参数或设置 MISTRAL_API_KEY 环境变量")
	}

	return nil
}

// updateConfigFromFlags 根据命令行参数更新配置
func updateConfigFromFlags(logger *zap.Logger) {
	if len(apiKeys) > 0 {
		logger.Debug("从命令行参数更新API密钥")
		cfg.APIKeys = apiKeys
	}
	if len(baseURLs) > 0 {
		logger.Debug("从命令行参数更新基础URL", zap.Strings("baseURLs", baseURLs))
		cfg.BaseURLs = baseURLs
	}
	if outputDir != "" {
		logger.Debug("从命令行参数更新输出目录", zap.String("outputDir", outputDir))
		cfg.OutputDir = outputDir
	}
	if logLevel != "" {
		logger.Debug("从命令行参数更新日志级别", zap.String("logLevel", logLevel))
		cfg.LogLevel = logLevel
	}
}

// loadCustomConfig 从指定路径加载配置
func loadCustomConfig(configPath string) (*config.Config, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", configPath)
	}
	return config.LoadConfigFromFile(configPath)
}

// generateConfig 生成默认配置
func generateConfig(cmd *cobra.Command, args []string) error {
	// 获取默认配置内容
	defaultConfig := config.GetDefaultConfig()

	if outputToFile == "" {
		// 输出到标准输出
		fmt.Println(defaultConfig)
	} else {
		// 确保目录存在
		dir := filepath.Dir(outputToFile)
		if dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
		}

		// 写入文件
		if err := os.WriteFile(outputToFile, []byte(defaultConfig), 0o644); err != nil {
			return fmt.Errorf("写入配置文件失败: %w", err)
		}
		fmt.Printf("配置已保存到: %s\n", outputToFile)
	}

	return nil
}

// processFile 处理本地PDF文件
func processFile(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		log.Info("处理单个文件或目录", zap.String("path", args[0]))
	} else {
		log.Info("处理多个文件或目录", zap.Strings("paths", args))
	}

	if dryRun {
		log.Info("空运行模式，不执行实际操作")
		return nil
	}

	// 创建OCR客户端
	client := ocr.NewClient(cfg.APIKeys, cfg.BaseURLs)
	client.SetTimeout(time.Duration(timeout) * time.Minute)
	client.SetMaxRetries(maxRetries)
	client.SetRetryDifferentEndpoint(cfg.RetryDifferentEndpoint)

	// 创建处理器
	processor := ocr.NewProcessor(client, log)

	if len(args) == 1 {
		// 检查是否为目录
		fileInfo, err := os.Stat(args[0])
		if err != nil {
			log.Error("获取文件信息失败", zap.Error(err))
			return err
		}

		if fileInfo.IsDir() {
			// 处理目录
			log.Info("处理目录中的所有PDF文件", zap.String("dir", args[0]))
			results, err := processor.ProcessMultipleFiles(args, ocr.ProcessOptions{
				IncludeImages:    cfg.IncludeImages,
				OutputDir:        cfg.OutputDir,
				CustomOutputName: outputName,
				ContinueOnError:  cfg.ContinueOnError,
			})
			if err != nil {
				log.Error("处理目录失败", zap.Error(err))
				return err
			}

			log.Info("目录处理完成", zap.Int("processed", len(results)))
			fmt.Printf("处理完成，共处理 %d 个文件\n", len(results))
			return nil
		}

		// 处理单个文件
		result, err := processor.ProcessFile(args[0], ocr.ProcessOptions{
			IncludeImages:    cfg.IncludeImages,
			OutputDir:        cfg.OutputDir,
			CustomOutputName: outputName,
		})
		if err != nil {
			log.Error("处理文件失败", zap.Error(err))
			return err
		}

		log.Info("处理完成", zap.String("outputDir", result.OutputDir))
		fmt.Printf("处理完成，结果保存在: %s\n", result.OutputDir)
		return nil
	} else {
		// 处理多个文件或目录
		results, err := processor.ProcessMultipleFiles(args, ocr.ProcessOptions{
			IncludeImages:    cfg.IncludeImages,
			OutputDir:        cfg.OutputDir,
			CustomOutputName: outputName,
			ContinueOnError:  cfg.ContinueOnError,
		})
		if err != nil {
			log.Error("处理多个文件或目录失败", zap.Error(err))
			return err
		}

		log.Info("所有文件处理完成", zap.Int("processed", len(results)))
		fmt.Printf("处理完成，共处理 %d 个文件\n", len(results))
		return nil
	}
}

// processURL 处理URL
func processURL(cmd *cobra.Command, args []string) error {
	urlStr := args[0]
	log.Info("处理URL", zap.String("url", urlStr))

	if dryRun {
		log.Info("空运行模式，不执行实际操作")
		return nil
	}

	// 验证URL
	_, err := url.ParseRequestURI(urlStr)
	if err != nil {
		log.Error("无效的URL", zap.Error(err))
		return fmt.Errorf("无效的URL: %w", err)
	}

	// 创建OCR客户端
	client := ocr.NewClient(cfg.APIKeys, cfg.BaseURLs)
	client.SetTimeout(time.Duration(timeout) * time.Minute)
	client.SetMaxRetries(maxRetries)
	client.SetRetryDifferentEndpoint(cfg.RetryDifferentEndpoint)

	// 创建处理器
	processor := ocr.NewProcessor(client, log)

	// 处理URL
	result, err := processor.ProcessURL(urlStr, ocr.ProcessOptions{
		IncludeImages:    cfg.IncludeImages,
		OutputDir:        cfg.OutputDir,
		CustomOutputName: outputName,
	})
	if err != nil {
		log.Error("处理URL失败", zap.Error(err))
		return err
	}

	log.Info("处理完成", zap.String("outputDir", result.OutputDir))
	fmt.Printf("处理完成，结果保存在: %s\n", result.OutputDir)
	return nil
}

// convertJSON 将JSON文件转换为Markdown
func convertJSON(cmd *cobra.Command, args []string) error {
	jsonPath := args[0]
	log.Info("转换JSON文件", zap.String("file", jsonPath))

	if dryRun {
		log.Info("空运行模式，不执行实际操作")
		return nil
	}

	// 创建OCR客户端 (转换不需要API密钥，但处理器需要客户端实例)
	client := ocr.NewClient(cfg.APIKeys, cfg.BaseURLs)
	client.SetRetryDifferentEndpoint(cfg.RetryDifferentEndpoint)

	// 创建处理器
	processor := ocr.NewProcessor(client, log)

	// 转换JSON
	result, err := processor.ConvertJSONToMarkdown(jsonPath, ocr.ProcessOptions{
		IncludeImages:    cfg.IncludeImages,
		OutputDir:        cfg.OutputDir,
		CustomOutputName: outputName,
	})
	if err != nil {
		log.Error("转换JSON失败", zap.Error(err))
		return err
	}

	log.Info("转换完成", zap.String("outputDir", result.OutputDir))
	fmt.Printf("转换完成，结果保存在: %s\n", result.OutputDir)
	return nil
}
