package ocr

import "encoding/json"

// OCRResponse 表示Mistral OCR API的响应
type OCRResponse struct {
	Pages     []Page `json:"pages"`
	Model     string `json:"model"`
	UsageInfo struct {
		PagesProcessed int  `json:"pages_processed"`
		DocSizeBytes   *int `json:"doc_size_bytes"`
	} `json:"usage_info"`

	// 原始响应数据，用于保存
	RawResponse []byte `json:"-"`
}

// Page 表示OCR响应中的单个页面
type Page struct {
	Index      int     `json:"index"`
	Markdown   string  `json:"markdown"`
	Images     []Image `json:"images"`
	Dimensions struct {
		DPI    int `json:"dpi"`
		Height int `json:"height"`
		Width  int `json:"width"`
	} `json:"dimensions"`
}

// Image 表示页面中的图像
type Image struct {
	ID           string `json:"id"`
	TopLeftX     int    `json:"top_left_x"`
	TopLeftY     int    `json:"top_left_y"`
	BottomRightX int    `json:"bottom_right_x"`
	BottomRightY int    `json:"bottom_right_y"`
	ImageBase64  string `json:"image_base64"`
}

// UploadResponse 表示上传文件时的响应
type UploadResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	CreatedAt int64  `json:"created_at"`
}

// SignedURLResponse 表示获取签名URL的响应
type SignedURLResponse struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	Object    string `json:"object,omitempty"`
	ID        string `json:"id,omitempty"`
}

// ProcessResult 表示处理结果
type ProcessResult struct {
	OutputDir    string
	ImagesDir    string
	MetadataPath string
	Pages        int
	ProcessedAt  string
}

// ProcessOptions 表示处理选项
type ProcessOptions struct {
	IncludeImages    bool
	OutputDir        string
	CustomOutputName string
	ContinueOnError  bool // 当处理多个文件时，如果一个文件处理失败，是否继续处理其他文件
}

// ProcessMetadata 存储处理元数据
type ProcessMetadata struct {
	SourceType      string          `json:"source_type"`       // "file" 或 "url"
	SourcePath      string          `json:"source_path"`       // 原始文件路径或URL
	OutputDir       string          `json:"output_dir"`        // 输出目录
	PagesProcessed  int             `json:"pages_processed"`   // 处理的页数
	ProcessedAt     string          `json:"processed_at"`      // 处理时间
	DocumentURL     string          `json:"document_url"`      // 文档URL
	FileID          string          `json:"file_id,omitempty"` // 文件ID（如果是上传的文件）
	IncludeImages   bool            `json:"include_images"`    // 是否包含图片
	ImagesSaved     int             `json:"images_saved"`      // 保存的图片数量
	OCRResponseInfo map[string]any  `json:"ocr_response_info"` // OCR响应信息
	RawResponse     json.RawMessage `json:"raw_response"`      // 原始OCR响应
}
