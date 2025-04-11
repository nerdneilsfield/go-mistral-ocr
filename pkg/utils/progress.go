package utils

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
)

// ProgressTracker 进度跟踪器
type ProgressTracker struct {
	bar       *progressbar.ProgressBar
	startTime time.Time
	title     string
	steps     int
	current   int
}

// NewProgressTracker 创建一个新的进度跟踪器
func NewProgressTracker(title string, steps int) *ProgressTracker {
	bar := progressbar.NewOptions(steps,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(50),
		progressbar.OptionSetDescription(fmt.Sprintf("[cyan]%s[reset]", title)),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
	)

	return &ProgressTracker{
		bar:       bar,
		startTime: time.Now(),
		title:     title,
		steps:     steps,
		current:   0,
	}
}

// Step 进度前进一步
func (pt *ProgressTracker) Step(description string) {
	pt.current++
	elapsed := time.Since(pt.startTime)
	descWithTime := fmt.Sprintf("%s (%s)", description, formatDuration(elapsed))
	pt.bar.Describe(fmt.Sprintf("[cyan]%s[reset] - %s", pt.title, descWithTime))
	pt.bar.Add(1)
}

// Complete 完成进度
func (pt *ProgressTracker) Complete() time.Duration {
	elapsed := time.Since(pt.startTime)
	// 确保进度条显示完成
	for pt.current < pt.steps {
		pt.Step("完成")
	}

	return elapsed
}

// formatDuration 格式化持续时间
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	parts := []string{}
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 || h > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	parts = append(parts, fmt.Sprintf("%ds", s))

	return strings.Join(parts, "")
}

// PrintResult 打印处理结果
func PrintResult(outputDir string, pages int, elapsed time.Duration) {
	fmt.Println()
	fmt.Println("✅ 处理完成!")
	fmt.Printf("📂 输出目录: %s\n", outputDir)
	fmt.Printf("📄 处理页数: %d\n", pages)
	fmt.Printf("⏱️ 处理时间: %s\n", formatDuration(elapsed))
	fmt.Println()
}

// IsTerminal 检查是否在终端中运行
func IsTerminal() bool {
	fileInfo, _ := os.Stdout.Stat()
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
