package ocr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 全局随机数生成器
var rnd = rand.New(rand.NewSource(time.Now().UnixNano()))

// Client 表示Mistral OCR API客户端
type Client struct {
	apiKeys                []string
	baseURLs               []string
	httpTimeout            time.Duration
	maxRetries             int
	currentKeyIndex        int
	currentURLIndex        int
	retryDifferentEndpoint bool
	mu                     sync.Mutex
}

// NewClient 创建一个新的Mistral OCR客户端
func NewClient(apiKeys []string, baseURLs []string) *Client {
	// 确保每个URL都以"/"结尾
	for i, baseURL := range baseURLs {
		if baseURL != "" && baseURL[len(baseURL)-1] != '/' {
			baseURLs[i] = baseURL + "/"
		}
	}

	// 随机选择初始的 API 密钥和 URL 索引
	var keyIndex, urlIndex int
	if len(apiKeys) > 0 {
		keyIndex = rnd.Intn(len(apiKeys))
	}
	if len(baseURLs) > 0 {
		urlIndex = rnd.Intn(len(baseURLs))
	}

	return &Client{
		apiKeys:                apiKeys,
		baseURLs:               baseURLs,
		httpTimeout:            5 * time.Minute, // 默认5分钟超时
		maxRetries:             3,               // 默认最多重试3次
		currentKeyIndex:        keyIndex,
		currentURLIndex:        urlIndex,
		retryDifferentEndpoint: true, // 默认启用不同端点重试
	}
}

// SetRetryDifferentEndpoint 设置是否在 API 调用失败时尝试使用不同的端点
func (c *Client) SetRetryDifferentEndpoint(retry bool) {
	c.retryDifferentEndpoint = retry
}

// getNextAPIKey 获取下一个要使用的API密钥
func (c *Client) getNextAPIKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.apiKeys) == 0 {
		return ""
	}

	apiKey := c.apiKeys[c.currentKeyIndex]
	c.currentKeyIndex = (c.currentKeyIndex + 1) % len(c.apiKeys)
	return apiKey
}

// getNextBaseURL 获取下一个要使用的基础URL
func (c *Client) getNextBaseURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.baseURLs) == 0 {
		return "https://api.mistral.ai/v1/"
	}

	baseURL := c.baseURLs[c.currentURLIndex]
	c.currentURLIndex = (c.currentURLIndex + 1) % len(c.baseURLs)
	return baseURL
}

// getCurrentBaseURL 获取当前的基础URL，不改变索引
func (c *Client) getCurrentBaseURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.baseURLs) == 0 {
		return "https://api.mistral.ai/v1/"
	}

	return c.baseURLs[c.currentURLIndex]
}

// SetTimeout 设置HTTP客户端超时时间
func (c *Client) SetTimeout(timeout time.Duration) {
	c.httpTimeout = timeout
}

// SetMaxRetries 设置最大重试次数
func (c *Client) SetMaxRetries(retries int) {
	c.maxRetries = retries
}

// UploadPDF 上传PDF文件到Mistral API
func (c *Client) UploadPDF(filePath string) (string, string, error) {
	// 获取文件信息
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return "", "", fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 记录文件大小
	fileSizeMB := float64(fileInfo.Size()) / 1024 / 1024
	fmt.Printf("开始上传文件: %s, 大小: %.2f MB\n", filePath, fileSizeMB)

	// 检查文件大小是否超过限制（50MB）
	if fileSizeMB > 50 {
		return "", "", fmt.Errorf("文件大小超过限制: %.2f MB > 50 MB", fileSizeMB)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", "", fmt.Errorf("无法打开文件: %w", err)
	}
	defer file.Close()

	var resp *http.Response
	var lastErr error
	var bodyBytes []byte
	var usedAPIKey string

	// 记录已尝试过的端点
	triedEndpoints := make(map[string]bool)

	// 外层循环：尝试不同的端点
	for endpointAttempt := 0; endpointAttempt < len(c.baseURLs); endpointAttempt++ {
		// 获取当前端点
		baseURL := c.getCurrentBaseURL()
		if triedEndpoints[baseURL] {
			// 如果已经尝试过这个端点，获取下一个
			baseURL = c.getNextBaseURL()
			if triedEndpoints[baseURL] {
				// 如果所有端点都已尝试过，退出
				if len(triedEndpoints) >= len(c.baseURLs) {
					break
				}
				continue
			}
		}
		triedEndpoints[baseURL] = true

		fmt.Printf("尝试使用端点: %s\n", baseURL)

		// 内层循环：在当前端点上进行重试
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			if attempt > 0 {
				// 指数退避策略，每次重试等待时间增加
				backoffTime := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
				fmt.Printf("第 %d 次重试，等待 %v 后重试...\n", attempt, backoffTime)
				time.Sleep(backoffTime)

				// 重新打开文件，因为前一次尝试可能已经读取了部分内容
				file.Seek(0, 0)
			}

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)

			// 添加表单字段 'purpose'
			err = writer.WriteField("purpose", "ocr")
			if err != nil {
				lastErr = fmt.Errorf("写入表单字段错误: %w", err)
				fmt.Printf("写入表单字段错误: %v\n", err)
				continue
			}

			// 添加文件
			part, err := writer.CreateFormFile("file", filepath.Base(filePath))
			if err != nil {
				lastErr = fmt.Errorf("创建表单文件错误: %w", err)
				fmt.Printf("创建表单文件错误: %v\n", err)
				continue
			}

			fmt.Printf("开始复制文件内容...\n")
			if _, err = io.Copy(part, file); err != nil {
				lastErr = fmt.Errorf("复制文件内容错误: %w", err)
				fmt.Printf("复制文件内容错误: %v\n", err)
				continue
			}

			if err = writer.Close(); err != nil {
				lastErr = fmt.Errorf("关闭表单写入器错误: %w", err)
				fmt.Printf("关闭表单写入器错误: %v\n", err)
				continue
			}

			// 获取当前使用的 API 密钥（打码处理）
			usedAPIKey = c.getNextAPIKey()
			maskedKey := "****"
			if len(usedAPIKey) > 8 {
				maskedKey = usedAPIKey[:4] + strings.Repeat("*", len(usedAPIKey)-8) + usedAPIKey[len(usedAPIKey)-4:]
			}

			fmt.Printf("创建请求: POST %sfiles, API密钥: %s\n", baseURL, maskedKey)
			req, err := http.NewRequest(http.MethodPost, baseURL+"files", body)
			if err != nil {
				lastErr = fmt.Errorf("创建请求错误: %w", err)
				fmt.Printf("创建请求错误: %v\n", err)
				continue
			}

			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+usedAPIKey)

			// 创建带超时的HTTP客户端
			client := &http.Client{
				Timeout: c.httpTimeout,
			}

			fmt.Printf("发送请求中...\n")
			resp, err = client.Do(req)
			if err != nil {
				lastErr = fmt.Errorf("发送请求错误: %w", err)
				fmt.Printf("发送请求错误: %v\n", err)
				continue
			}

			// 读取响应体
			fmt.Printf("收到响应，状态码: %d\n", resp.StatusCode)
			bodyBytes, err = io.ReadAll(resp.Body)
			resp.Body.Close()

			if err != nil {
				lastErr = fmt.Errorf("读取响应体错误: %w", err)
				fmt.Printf("读取响应体错误: %v\n", err)
				continue
			}

			// 检查状态码
			if resp.StatusCode == http.StatusOK {
				// 成功，跳出重试循环
				var uploadResp UploadResponse
				err = json.Unmarshal(bodyBytes, &uploadResp)
				if err != nil {
					fmt.Printf("解析响应错误: %v\n", err)
					return "", "", fmt.Errorf("解析响应错误: %w", err)
				}
				fmt.Printf("上传成功，文件ID: %s\n", uploadResp.ID)
				return uploadResp.ID, usedAPIKey, nil
			} else if resp.StatusCode == http.StatusGatewayTimeout || resp.StatusCode == http.StatusServiceUnavailable {
				// 服务器超时或不可用，继续重试
				lastErr = fmt.Errorf("上传失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("服务器错误，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				continue
			} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				// 认证错误，尝试下一个API密钥
				lastErr = fmt.Errorf("上传失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("认证错误，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				break // 跳出内层循环，尝试下一个端点
			} else {
				// 其他错误，如果启用了不同端点重试，则尝试下一个端点
				lastErr = fmt.Errorf("上传失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("请求失败，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				if c.retryDifferentEndpoint {
					fmt.Printf("将尝试使用不同端点重试\n")
					break // 跳出内层循环，尝试下一个端点
				} else {
					return "", "", lastErr // 不尝试其他端点，直接返回错误
				}
			}
		}

		// 如果没有启用不同端点重试，或者已经成功，则退出外层循环
		if !c.retryDifferentEndpoint {
			break
		}
	}

	// 如果所有尝试都失败
	fmt.Printf("所有尝试均失败，最后错误: %v\n", lastErr)
	return "", "", lastErr
}

// GetSignedURL 获取上传文件的签名URL
func (c *Client) GetSignedURL(fileID string, apiKey string) (string, error) {
	fmt.Printf("获取文件签名URL，文件ID: %s\n", fileID)

	var resp *http.Response
	var lastErr error
	var bodyBytes []byte

	// 记录已尝试过的端点
	triedEndpoints := make(map[string]bool)

	// 外层循环：尝试不同的端点
	for endpointAttempt := 0; endpointAttempt < len(c.baseURLs); endpointAttempt++ {
		// 获取当前端点
		baseURL := c.getCurrentBaseURL()
		if triedEndpoints[baseURL] {
			// 如果已经尝试过这个端点，获取下一个
			baseURL = c.getNextBaseURL()
			if triedEndpoints[baseURL] {
				// 如果所有端点都已尝试过，退出
				if len(triedEndpoints) >= len(c.baseURLs) {
					break
				}
				continue
			}
		}
		triedEndpoints[baseURL] = true

		fmt.Printf("尝试使用端点: %s\n", baseURL)

		// 内层循环：在当前端点上进行重试
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			if attempt > 0 {
				// 指数退避策略，每次重试等待时间增加
				backoffTime := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
				fmt.Printf("第 %d 次重试，等待 %v 后重试...\n", attempt, backoffTime)
				time.Sleep(backoffTime)
			}

			// 使用传入的 API 密钥（打码处理）
			maskedKey := "****"
			if len(apiKey) > 8 {
				maskedKey = apiKey[:4] + strings.Repeat("*", len(apiKey)-8) + apiKey[len(apiKey)-4:]
			}

			requestURL := baseURL + "files/" + fileID + "/url?expiry=24"
			fmt.Printf("创建请求: GET %s, API密钥: %s\n", requestURL, maskedKey)

			req, err := http.NewRequest(http.MethodGet, requestURL, nil)
			if err != nil {
				lastErr = fmt.Errorf("创建请求错误: %w", err)
				fmt.Printf("创建请求错误: %v\n", err)
				continue
			}

			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Accept", "application/json")

			// 创建带超时的HTTP客户端
			client := &http.Client{
				Timeout: c.httpTimeout,
			}

			fmt.Printf("发送请求中...\n")
			resp, err = client.Do(req)
			if err != nil {
				lastErr = fmt.Errorf("发送请求错误: %w", err)
				fmt.Printf("发送请求错误: %v\n", err)
				continue
			}

			// 读取响应体
			fmt.Printf("收到响应，状态码: %d\n", resp.StatusCode)
			bodyBytes, err = io.ReadAll(resp.Body)
			resp.Body.Close()

			if err != nil {
				lastErr = fmt.Errorf("读取响应体错误: %w", err)
				fmt.Printf("读取响应体错误: %v\n", err)
				continue
			}

			// 检查状态码
			if resp.StatusCode == http.StatusOK {
				// 成功，解析响应
				var signedURLResp SignedURLResponse
				err := json.Unmarshal(bodyBytes, &signedURLResp)
				if err != nil {
					fmt.Printf("解析响应错误: %v\n", err)
					return "", fmt.Errorf("解析响应错误: %w", err)
				}
				fmt.Printf("获取签名URL成功: %s\n", signedURLResp.URL)
				return signedURLResp.URL, nil
			} else if resp.StatusCode == http.StatusGatewayTimeout || resp.StatusCode == http.StatusServiceUnavailable {
				// 服务器超时或不可用，继续重试
				lastErr = fmt.Errorf("获取签名URL失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("服务器错误，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				continue
			} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				// 认证错误，尝试下一个API密钥
				lastErr = fmt.Errorf("获取签名URL失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("认证错误，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				break // 跳出内层循环，尝试下一个端点
			} else {
				// 其他错误，如果启用了不同端点重试，则尝试下一个端点
				lastErr = fmt.Errorf("获取签名URL失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("请求失败，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				if c.retryDifferentEndpoint {
					fmt.Printf("将尝试使用不同端点重试\n")
					break // 跳出内层循环，尝试下一个端点
				} else {
					return "", lastErr // 不尝试其他端点，直接返回错误
				}
			}
		}

		// 如果没有启用不同端点重试，则退出外层循环
		if !c.retryDifferentEndpoint {
			break
		}
	}

	// 如果所有尝试都失败
	fmt.Printf("所有尝试均失败，最后错误: %v\n", lastErr)
	return "", lastErr
}

// ProcessOCR 使用OCR处理文档
func (c *Client) ProcessOCR(documentURL string, includeImageBase64 bool, apiKey string) (*OCRResponse, error) {
	fmt.Printf("开始OCR处理文档，URL: %s\n", documentURL)

	// 检查是否为有效URL
	_, err := url.ParseRequestURI(documentURL)
	if err != nil {
		fmt.Printf("无效的URL: %v\n", err)
		return nil, fmt.Errorf("无效的URL: %w", err)
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"model": "mistral-ocr-latest",
		"document": map[string]string{
			"type":         "document_url",
			"document_url": documentURL,
		},
		"include_image_base64": includeImageBase64,
	})
	if err != nil {
		fmt.Printf("创建请求体错误: %v\n", err)
		return nil, fmt.Errorf("创建请求体错误: %w", err)
	}

	fmt.Printf("请求体: %s\n", string(requestBody))

	var resp *http.Response
	var lastErr error
	var bodyBytes []byte

	// 记录已尝试过的端点
	triedEndpoints := make(map[string]bool)

	// 外层循环：尝试不同的端点
	for endpointAttempt := 0; endpointAttempt < len(c.baseURLs); endpointAttempt++ {
		// 获取当前端点
		baseURL := c.getCurrentBaseURL()
		if triedEndpoints[baseURL] {
			// 如果已经尝试过这个端点，获取下一个
			baseURL = c.getNextBaseURL()
			if triedEndpoints[baseURL] {
				// 如果所有端点都已尝试过，退出
				if len(triedEndpoints) >= len(c.baseURLs) {
					break
				}
				continue
			}
		}
		triedEndpoints[baseURL] = true

		fmt.Printf("尝试使用端点: %s\n", baseURL)

		// 内层循环：在当前端点上进行重试
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			if attempt > 0 {
				// 指数退避策略，每次重试等待时间增加
				backoffTime := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
				fmt.Printf("第 %d 次重试，等待 %v 后重试...\n", attempt, backoffTime)
				time.Sleep(backoffTime)
			}

			// 使用传入的 API 密钥（打码处理）
			maskedKey := "****"
			if len(apiKey) > 8 {
				maskedKey = apiKey[:4] + strings.Repeat("*", len(apiKey)-8) + apiKey[len(apiKey)-4:]
			}

			fmt.Printf("创建请求: POST %socr, API密钥: %s\n", baseURL, maskedKey)
			req, err := http.NewRequest(http.MethodPost, baseURL+"ocr", bytes.NewBuffer(requestBody))
			if err != nil {
				lastErr = fmt.Errorf("创建请求错误: %w", err)
				fmt.Printf("创建请求错误: %v\n", err)
				continue
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+apiKey)

			// 创建带超时的HTTP客户端
			client := &http.Client{
				Timeout: c.httpTimeout,
			}

			fmt.Printf("发送请求中...\n")
			resp, err = client.Do(req)
			if err != nil {
				lastErr = fmt.Errorf("发送请求错误: %w", err)
				fmt.Printf("发送请求错误: %v\n", err)
				continue
			}

			// 读取响应体
			fmt.Printf("收到响应，状态码: %d\n", resp.StatusCode)
			bodyBytes, err = io.ReadAll(resp.Body)
			resp.Body.Close()

			if err != nil {
				lastErr = fmt.Errorf("读取响应体错误: %w", err)
				fmt.Printf("读取响应体错误: %v\n", err)
				continue
			}

			// 检查状态码
			if resp.StatusCode == http.StatusOK {
				// 成功，解析响应
				var ocrResp OCRResponse
				err = json.Unmarshal(bodyBytes, &ocrResp)
				if err != nil {
					fmt.Printf("解析响应错误: %v\n", err)
					return nil, fmt.Errorf("解析响应错误: %w", err)
				}

				// 设置原始响应
				ocrResp.RawResponse = bodyBytes

				fmt.Printf("OCR处理成功，共 %d 页\n", len(ocrResp.Pages))
				return &ocrResp, nil
			} else if resp.StatusCode == http.StatusGatewayTimeout || resp.StatusCode == http.StatusServiceUnavailable {
				// 服务器超时或不可用，继续重试
				lastErr = fmt.Errorf("OCR处理失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("服务器错误，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				continue
			} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				// 认证错误，尝试下一个API密钥
				lastErr = fmt.Errorf("OCR处理失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("认证错误，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				break // 跳出内层循环，尝试下一个端点
			} else {
				// 其他错误，如果启用了不同端点重试，则尝试下一个端点
				lastErr = fmt.Errorf("OCR处理失败，状态码 %d: %s", resp.StatusCode, string(bodyBytes))
				fmt.Printf("请求失败，状态码: %d, 响应: %s\n", resp.StatusCode, string(bodyBytes))
				if c.retryDifferentEndpoint {
					fmt.Printf("将尝试使用不同端点重试\n")
					break // 跳出内层循环，尝试下一个端点
				} else {
					return nil, lastErr // 不尝试其他端点，直接返回错误
				}
			}
		}

		// 如果没有启用不同端点重试，则退出外层循环
		if !c.retryDifferentEndpoint {
			break
		}
	}

	// 如果所有尝试都失败
	fmt.Printf("所有尝试均失败，最后错误: %v\n", lastErr)
	return nil, lastErr
}
