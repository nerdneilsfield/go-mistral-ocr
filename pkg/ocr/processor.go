package ocr

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Processor 处理OCR结果
type Processor struct {
	client *Client
	logger *zap.Logger
}

// NewProcessor 创建一个新的处理器
func NewProcessor(client *Client, logger *zap.Logger) *Processor {
	return &Processor{
		client: client,
		logger: logger,
	}
}

// checkOutputDir 检查输出目录是否已经存在并且output.md不为空
func (p *Processor) checkOutputDir(outputDir string) (bool, error) {
	// 检查输出目录是否存在
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		return false, nil
	}

	// 检查output.md文件是否存在且不为空
	mdPath := filepath.Join(outputDir, "output.md")
	fileInfo, err := os.Stat(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("检查output.md文件失败: %w", err)
	}

	// 如果文件大小为0，则认为需要重新处理
	if fileInfo.Size() == 0 {
		return false, nil
	}

	return true, nil
}

// ProcessFile 处理文件并返回结果
func (p *Processor) ProcessFile(filePath string, opts ProcessOptions) (*ProcessResult, error) {
	startTime := time.Now()
	p.logger.Info("开始处理文件", zap.String("filePath", filePath))

	// 确定输出文件名
	outputName := opts.CustomOutputName
	if outputName == "" {
		// 使用原始文件名(不带扩展名)
		outputName = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	}

	// 创建输出目录
	outputDir := filepath.Join(opts.OutputDir, outputName)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录错误: %w", err)
	}

	// 检查输出目录是否已经存在并且output.md不为空
	exists, err := p.checkOutputDir(outputDir)
	if err != nil {
		return nil, fmt.Errorf("检查输出目录失败: %w", err)
	}
	if exists {
		p.logger.Info("输出目录已存在且output.md不为空，跳过处理", zap.String("outputDir", outputDir))
		return &ProcessResult{
			OutputDir:    outputDir,
			ImagesDir:    filepath.Join(outputDir, "images"),
			MetadataPath: filepath.Join(outputDir, "metadata.json"),
			Pages:        0,
			ProcessedAt:  "0s",
		}, nil
	}

	// 创建元数据
	metadata := ProcessMetadata{
		SourceType:    "file",
		SourcePath:    filePath,
		OutputDir:     opts.OutputDir,
		ProcessedAt:   startTime.Format(time.RFC3339),
		IncludeImages: opts.IncludeImages,
	}

	// 上传PDF文件
	p.logger.Debug("上传PDF文件...")
	fileID, apiKey, err := p.client.UploadPDF(filePath)
	if err != nil {
		p.logger.Error("上传PDF文件失败", zap.Error(err), zap.String("filePath", filePath))
		return nil, fmt.Errorf("上传PDF文件失败: %w", err)
	}
	metadata.FileID = fileID
	p.logger.Debug("文件已上传", zap.String("fileID", fileID))

	// 获取签名URL
	p.logger.Debug("获取签名URL...")
	signedURL, err := p.client.GetSignedURL(fileID, apiKey)
	if err != nil {
		p.logger.Error("获取签名URL失败", zap.Error(err), zap.String("fileID", fileID))
		return nil, fmt.Errorf("获取签名URL失败: %w", err)
	}
	metadata.DocumentURL = signedURL
	p.logger.Debug("获取到签名URL", zap.String("url", signedURL))

	// 使用OCR处理文档
	return p.processDocument(signedURL, filePath, opts, metadata, startTime, apiKey)
}

// ProcessURL 直接处理URL
func (p *Processor) ProcessURL(documentURL string, opts ProcessOptions) (*ProcessResult, error) {
	startTime := time.Now()
	p.logger.Info("开始处理URL", zap.String("url", documentURL))

	// 创建元数据
	metadata := ProcessMetadata{
		SourceType:    "url",
		SourcePath:    documentURL,
		OutputDir:     opts.OutputDir,
		ProcessedAt:   startTime.Format(time.RFC3339),
		IncludeImages: opts.IncludeImages,
		DocumentURL:   documentURL,
	}

	// 使用OCR处理文档 - 对于直接URL，我们可以使用随机的API密钥
	apiKey := p.client.getNextAPIKey()
	return p.processDocument(documentURL, "", opts, metadata, startTime, apiKey)
}

// processDocument 处理文档并返回结果
func (p *Processor) processDocument(documentURL string, originalFile string, opts ProcessOptions, metadata ProcessMetadata, startTime time.Time, apiKey string) (*ProcessResult, error) {
	// 使用OCR处理文档
	p.logger.Debug("进行OCR处理...")
	ocrResponse, err := p.client.ProcessOCR(documentURL, opts.IncludeImages, apiKey)
	if err != nil {
		p.logger.Error("OCR处理失败", zap.Error(err), zap.String("documentURL", documentURL))
		return nil, fmt.Errorf("OCR处理失败: %w", err)
	}
	p.logger.Debug("OCR处理完成", zap.Int("pages", len(ocrResponse.Pages)))

	// 确定输出文件名
	outputName := opts.CustomOutputName
	if outputName == "" && originalFile != "" {
		// 使用原始文件名(不带扩展名)
		outputName = strings.TrimSuffix(filepath.Base(originalFile), filepath.Ext(originalFile))
	} else if outputName == "" {
		// 使用时间戳作为默认名称
		outputName = fmt.Sprintf("ocr-result-%d", time.Now().Unix())
	}

	// 创建输出目录
	outputDir := filepath.Join(opts.OutputDir, outputName)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录错误: %w", err)
	}

	// 更新元数据
	metadata.PagesProcessed = len(ocrResponse.Pages)
	metadata.OutputDir = outputDir
	metadata.OCRResponseInfo = map[string]any{
		"model":           ocrResponse.Model,
		"pages_processed": ocrResponse.UsageInfo.PagesProcessed,
	}
	if ocrResponse.UsageInfo.DocSizeBytes != nil {
		metadata.OCRResponseInfo["doc_size_bytes"] = *ocrResponse.UsageInfo.DocSizeBytes
	}

	// 设置原始响应到元数据
	if ocrResponse.RawResponse != nil {
		metadata.RawResponse = json.RawMessage(ocrResponse.RawResponse)
	}

	// 处理并保存结果
	result, err := p.saveResults(ocrResponse, outputDir, metadata, opts.IncludeImages)
	if err != nil {
		return nil, fmt.Errorf("保存结果失败: %w", err)
	}

	elapsedTime := time.Since(startTime)
	result.ProcessedAt = elapsedTime.String()
	p.logger.Info("处理完成",
		zap.String("outputDir", result.OutputDir),
		zap.Int("pages", result.Pages),
		zap.String("processTime", result.ProcessedAt))

	return result, nil
}

// saveResults 保存OCR处理结果
func (p *Processor) saveResults(resp *OCRResponse, outputDir string, metadata ProcessMetadata, includeImages bool) (*ProcessResult, error) {
	var allMarkdown strings.Builder
	var allText strings.Builder
	imageCount := 0
	imagesDir := outputDir

	// 如果需要保存图片，创建images子目录
	if includeImages {
		imagesDir = filepath.Join(outputDir, "images")
		if err := os.MkdirAll(imagesDir, 0755); err != nil {
			return nil, fmt.Errorf("创建images子目录错误: %w", err)
		}
	}

	// 图片ID到本地路径的映射
	imageMap := make(map[string]string)

	// 保存图片（如果有）
	if includeImages {
		for _, page := range resp.Pages {
			for _, img := range page.Images {
				if img.ImageBase64 != "" && img.ImageBase64 != "..." {
					// 处理data:image/jpeg;base64,格式的图片数据
					imgData := img.ImageBase64
					// 检查是否是Data URL格式
					if strings.HasPrefix(imgData, "data:") {
						// 提取base64部分
						parts := strings.Split(imgData, ",")
						if len(parts) == 2 {
							imgData = parts[1]
							// 处理URL编码的换行符
							imgData = strings.ReplaceAll(imgData, "\n", "")
							imgData = strings.ReplaceAll(imgData, "\r", "")
							// 移除所有空白字符
							imgData = strings.ReplaceAll(imgData, " ", "")
						} else {
							p.logger.Warn("解析图片数据URL格式失败", zap.String("imageID", img.ID))
							continue
						}
					}

					// 解码base64数据
					decodedData, err := base64.StdEncoding.DecodeString(imgData)
					if err != nil {
						p.logger.Warn("解码图片失败", zap.String("imageID", img.ID), zap.Error(err))
						continue
					}

					// 确定图片文件名
					imgFilename := img.ID
					if !strings.Contains(imgFilename, ".") {
						imgFilename += ".jpeg" // 添加默认扩展名
					}

					imgPath := filepath.Join(imagesDir, imgFilename)
					if err = os.WriteFile(imgPath, decodedData, 0644); err != nil {
						p.logger.Warn("保存图片失败", zap.String("imageID", img.ID), zap.Error(err))
						continue
					}

					// 记录图片ID到相对路径的映射
					imageMap[img.ID] = filepath.Join("images", imgFilename)
					imageCount++
					p.logger.Debug("保存图片", zap.String("imageID", img.ID), zap.String("path", imgPath))
				}
			}
		}
	}

	// 更新元数据中的图片计数
	metadata.ImagesSaved = imageCount

	// 处理每个页面的内容
	for i, page := range resp.Pages {
		p.logger.Debug("处理页面", zap.Int("pageNum", i+1))

		// 替换markdown中的图片链接（如果有图片）
		markdown := page.Markdown
		if includeImages {
			for imgID, localPath := range imageMap {
				// 替换形如 ![img-0.jpeg](img-0.jpeg) 的链接
				markdown = strings.ReplaceAll(markdown,
					"!["+imgID+"]("+imgID+")",
					"!["+imgID+"]("+localPath+")")
			}
		}

		allMarkdown.WriteString(markdown)
		allMarkdown.WriteString("\n\n")

		// 提取文本
		text := extractTextFromMarkdown(markdown)
		allText.WriteString(text)
		allText.WriteString("\n\n")
	}

	// 保存元数据到JSON文件
	metadataPath := filepath.Join(outputDir, "metadata.json")
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		p.logger.Warn("保存元数据失败", zap.Error(err))
	} else {
		if err := os.WriteFile(metadataPath, metadataJSON, 0644); err != nil {
			p.logger.Warn("写入元数据文件失败", zap.Error(err))
		} else {
			p.logger.Debug("保存了元数据文件", zap.String("path", metadataPath))
		}
	}

	// 保存markdown
	mdPath := filepath.Join(outputDir, "output.md")
	if err := os.WriteFile(mdPath, []byte(allMarkdown.String()), 0644); err != nil {
		return nil, fmt.Errorf("保存markdown输出错误: %w", err)
	}
	p.logger.Debug("保存了markdown文件", zap.String("path", mdPath))

	// 保存文本
	txtPath := filepath.Join(outputDir, "output.txt")
	if err := os.WriteFile(txtPath, []byte(allText.String()), 0644); err != nil {
		return nil, fmt.Errorf("保存文本输出错误: %w", err)
	}
	p.logger.Debug("保存了文本文件", zap.String("path", txtPath))

	return &ProcessResult{
		OutputDir:    outputDir,
		ImagesDir:    imagesDir,
		MetadataPath: metadataPath,
		Pages:        len(resp.Pages),
	}, nil
}

// extractTextFromMarkdown 从markdown提取纯文本内容
func extractTextFromMarkdown(markdown string) string {
	// 移除图片链接
	result := markdown

	// 查找并移除 ![...](...)
	startIdx := 0
	for {
		imgStart := strings.Index(result[startIdx:], "![")
		if imgStart == -1 {
			break
		}
		imgStart += startIdx

		closeBracket := strings.Index(result[imgStart:], ")")
		if closeBracket == -1 {
			break
		}
		closeBracket += imgStart + 1

		// 移除图片标记
		result = result[:imgStart] + result[closeBracket:]
		startIdx = imgStart
	}

	// 简单处理markdown格式
	result = strings.ReplaceAll(result, "\n\n", "\n")

	return result
}

// ConvertJSONToMarkdown 从JSON文件生成Markdown文件
func (p *Processor) ConvertJSONToMarkdown(jsonFilePath string, opts ProcessOptions) (*ProcessResult, error) {
	startTime := time.Now()
	p.logger.Info("开始从JSON文件生成Markdown", zap.String("jsonFile", jsonFilePath))

	// 读取JSON文件
	jsonData, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return nil, fmt.Errorf("读取JSON文件失败: %w", err)
	}

	// 解析JSON数据
	var ocrResponse OCRResponse
	if err := json.Unmarshal(jsonData, &ocrResponse); err != nil {
		return nil, fmt.Errorf("解析JSON数据失败: %w", err)
	}

	// 检查是否需要从raw_response中提取pages数据
	if len(ocrResponse.Pages) == 0 {
		// 尝试从raw_response中提取pages数据
		var rawResponse map[string]interface{}
		if err := json.Unmarshal(jsonData, &rawResponse); err != nil {
			return nil, fmt.Errorf("解析raw_response数据失败: %w", err)
		}

		// 检查raw_response中是否包含pages字段
		if rawResponseData, ok := rawResponse["raw_response"].(map[string]interface{}); ok {
			if pagesData, ok := rawResponseData["pages"].([]interface{}); ok {
				p.logger.Debug("从raw_response中提取pages数据", zap.Int("pages_count", len(pagesData)))

				// 将pages数据转换为OCRResponse.Pages
				for pageIndex, pageData := range pagesData {
					if pageMap, ok := pageData.(map[string]interface{}); ok {
						page := Page{}

						// 提取index
						if index, ok := pageMap["index"].(float64); ok {
							page.Index = int(index)
						}

						// 提取markdown
						if markdown, ok := pageMap["markdown"].(string); ok {
							page.Markdown = markdown
						}

						// 提取images
						if imagesData, ok := pageMap["images"].([]interface{}); ok {
							p.logger.Debug("提取images数据", zap.Int("images_count", len(imagesData)), zap.Int("page_index", pageIndex))
							for _, imgData := range imagesData {
								if imgMap, ok := imgData.(map[string]interface{}); ok {
									image := Image{}

									// 提取image_id
									if id, ok := imgMap["id"].(string); ok {
										image.ID = id
									}

									// 提取坐标
									if x, ok := imgMap["top_left_x"].(float64); ok {
										image.TopLeftX = int(x)
									}
									if y, ok := imgMap["top_left_y"].(float64); ok {
										image.TopLeftY = int(y)
									}
									if x, ok := imgMap["bottom_right_x"].(float64); ok {
										image.BottomRightX = int(x)
									}
									if y, ok := imgMap["bottom_right_y"].(float64); ok {
										image.BottomRightY = int(y)
									}

									// 提取base64图像数据
									if base64Data, ok := imgMap["image_base64"].(string); ok {
										image.ImageBase64 = base64Data
									}

									page.Images = append(page.Images, image)
								}
							}
						}

						// 添加到pages数组
						ocrResponse.Pages = append(ocrResponse.Pages, page)
					}
				}

				p.logger.Debug("成功从raw_response提取pages数据", zap.Int("extracted_pages", len(ocrResponse.Pages)))
			}
		}
	}

	ocrResponse.RawResponse = jsonData

	// 确定输出文件名
	outputName := opts.CustomOutputName
	if outputName == "" {
		// 使用原始文件名(不带扩展名)
		outputName = strings.TrimSuffix(filepath.Base(jsonFilePath), filepath.Ext(jsonFilePath))
	}

	// 创建输出目录
	outputDir := filepath.Join(opts.OutputDir, outputName)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}
	p.logger.Debug("创建输出目录", zap.String("dir", outputDir))

	// 创建元数据
	metadata := ProcessMetadata{
		SourceType:     "json",
		SourcePath:     jsonFilePath,
		OutputDir:      outputDir,
		ProcessedAt:    startTime.Format(time.RFC3339),
		IncludeImages:  opts.IncludeImages,
		PagesProcessed: len(ocrResponse.Pages),
		OCRResponseInfo: map[string]any{
			"model":           ocrResponse.Model,
			"pages_processed": ocrResponse.UsageInfo.PagesProcessed,
		},
		RawResponse: ocrResponse.RawResponse,
	}

	// 保存结果
	result, err := p.saveResults(&ocrResponse, outputDir, metadata, opts.IncludeImages)
	if err != nil {
		return nil, fmt.Errorf("保存结果失败: %w", err)
	}

	result.ProcessedAt = metadata.ProcessedAt
	p.logger.Info("转换完成",
		zap.String("outputDir", result.OutputDir),
		zap.Int("pages", result.Pages))

	return result, nil
}

// ProcessMultipleFiles 处理多个PDF文件或目录中的所有PDF文件
func (p *Processor) ProcessMultipleFiles(paths []string, opts ProcessOptions) ([]*ProcessResult, error) {
	var results []*ProcessResult
	var filesToProcess []string
	var errors []error
	var skippedFiles int

	// 收集所有需要处理的文件
	for _, path := range paths {
		fileInfo, err := os.Stat(path)
		if err != nil {
			p.logger.Error("获取文件信息失败", zap.String("path", path), zap.Error(err))
			if !opts.ContinueOnError {
				return nil, fmt.Errorf("获取文件信息失败: %w", err)
			}
			errors = append(errors, fmt.Errorf("获取文件信息失败 %s: %w", path, err))
			continue
		}

		if fileInfo.IsDir() {
			// 如果是目录，收集目录中所有的PDF文件
			p.logger.Info("扫描目录中的PDF文件", zap.String("dir", path))
			err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() && strings.ToLower(filepath.Ext(filePath)) == ".pdf" {
					filesToProcess = append(filesToProcess, filePath)
				}
				return nil
			})
			if err != nil {
				p.logger.Error("扫描目录失败", zap.String("dir", path), zap.Error(err))
				if !opts.ContinueOnError {
					return nil, fmt.Errorf("扫描目录失败: %w", err)
				}
				errors = append(errors, fmt.Errorf("扫描目录失败 %s: %w", path, err))
				continue
			}
		} else if strings.ToLower(filepath.Ext(path)) == ".pdf" {
			// 如果是PDF文件，直接添加到处理列表
			filesToProcess = append(filesToProcess, path)
		} else {
			p.logger.Warn("跳过非PDF文件", zap.String("file", path))
		}
	}

	if len(filesToProcess) == 0 {
		if len(errors) > 0 {
			return nil, fmt.Errorf("没有找到可处理的PDF文件，发生了 %d 个错误", len(errors))
		}
		return nil, fmt.Errorf("没有找到可处理的PDF文件")
	}

	p.logger.Info("开始处理文件", zap.Int("total", len(filesToProcess)))

	// 处理每个文件
	for i, filePath := range filesToProcess {
		p.logger.Info("处理文件", zap.Int("current", i+1), zap.Int("total", len(filesToProcess)), zap.String("file", filePath))

		// 为每个文件创建单独的输出名称
		fileOpts := opts
		if fileOpts.CustomOutputName == "" {
			// 使用文件名作为输出名称
			fileOpts.CustomOutputName = strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		} else if len(filesToProcess) > 1 {
			// 如果处理多个文件但指定了输出名称，则添加序号
			fileOpts.CustomOutputName = fmt.Sprintf("%s_%d", fileOpts.CustomOutputName, i+1)
		}

		result, err := p.ProcessFile(filePath, fileOpts)
		if err != nil {
			p.logger.Error("处理文件失败", zap.String("file", filePath), zap.Error(err))
			errors = append(errors, fmt.Errorf("处理文件失败 %s: %w", filePath, err))
			// 如果不继续处理，则返回错误
			if !opts.ContinueOnError {
				return results, fmt.Errorf("处理文件失败: %w", err)
			}
			// 继续处理其他文件，不中断整个过程
			continue
		}

		// 如果结果中的页数为0，说明文件被跳过了
		if result.Pages == 0 {
			skippedFiles++
		}

		results = append(results, result)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("所有文件处理失败，发生了 %d 个错误", len(errors))
	}

	// 如果有错误但仍然处理了一些文件，记录错误数量
	if len(errors) > 0 {
		p.logger.Warn("部分文件处理失败", zap.Int("success", len(results)), zap.Int("failed", len(errors)), zap.Int("total", len(filesToProcess)))
	}

	p.logger.Info("所有文件处理完成",
		zap.Int("success", len(results)),
		zap.Int("skipped", skippedFiles),
		zap.Int("total", len(filesToProcess)))
	return results, nil
}
