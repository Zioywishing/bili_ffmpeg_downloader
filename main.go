package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Define a client with a timeout
var httpClient = &http.Client{
	Timeout: 30 * time.Second, // 30-second timeout
}

func createCache() {
	cacheDir := filepath.Join(".", ".cache")
	fmt.Println("尝试创建缓存目录:", cacheDir)
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		err := os.MkdirAll(cacheDir, 0755)
		if err != nil {
			fmt.Println("创建缓存目录失败:", err)
			// Decide if you want to exit here or let downloadFile handle it
			// os.Exit(1)
			return // Return early if creation fails
		}
		fmt.Println("缓存目录创建成功")
	} else {
		fmt.Println("缓存目录已存在")
	}
}

func clearCache() {
	cacheDir := filepath.Join(".", ".cache")
	if _, err := os.Stat(cacheDir); err == nil {
		os.RemoveAll(cacheDir)
	}
}

func randomName() string {
	return fmt.Sprintf("%d", rand.Int63n(int64(math.Pow10(10))))
}

func combineAudioAndVideo(vPath, aPath string, outputPath string) (string, error) {
	if outputPath == "" {
		outputPath = fmt.Sprintf("%s.mp4", randomName())
	}

	outputDir := filepath.Dir(outputPath)
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		os.MkdirAll(outputDir, 0755)
	}

	cmd := exec.Command("ffmpeg",
		"-i", vPath,
		"-i", aPath,
		"-c:v", "copy",
		"-c:a", "aac",
		outputPath)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var duration float64 = 0
	scanner := bufio.NewScanner(stderr)
	durationRegex := regexp.MustCompile(`Duration: (\d{2}):(\d{2}):(\d{2})`)
	timeRegex := regexp.MustCompile(`time=(\d{2}):(\d{2}):(\d{2})`)

	for scanner.Scan() {
		output := scanner.Text()

		// 尝试获取视频总时长
		if duration == 0 {
			durationMatch := durationRegex.FindStringSubmatch(output)
			if len(durationMatch) > 0 {
				hours, _ := strconv.Atoi(durationMatch[1])
				minutes, _ := strconv.Atoi(durationMatch[2])
				seconds, _ := strconv.Atoi(durationMatch[3])
				duration = float64(hours*3600 + minutes*60 + seconds)
			}
		}

		// 尝试获取当前处理时间
		timeMatch := timeRegex.FindStringSubmatch(output)
		if len(timeMatch) > 0 && duration > 0 {
			hours, _ := strconv.Atoi(timeMatch[1])
			minutes, _ := strconv.Atoi(timeMatch[2])
			seconds, _ := strconv.Atoi(timeMatch[3])
			currentTime := float64(hours*3600 + minutes*60 + seconds)

			// 计算进度
			progress := (currentTime / duration) * 100

			// 显示进度条
			progressBarWidth := 50
			filledWidth := int(math.Round((progress / 100) * float64(progressBarWidth)))
			progressBar := strings.Repeat("█", filledWidth) + strings.Repeat("░", progressBarWidth-filledWidth)

			fmt.Printf("\r合并进度: [%s] %.2f%% | 当前: %s", progressBar, progress, strings.Replace(timeMatch[0], "time=", "", 1))
		}
	}

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("FFmpeg进程退出，错误码 %v", err)
	}

	fmt.Print("\r合并完成                                                       \n")
	return outputPath, nil
}

type downloadRecord struct {
	timestamp int64
	bytes     int
}

type ProgressInfo struct {
	Percentage   float64
	DownloadedMB float64
	TotalMB      float64
	SpeedMBps    float64
	ProgressBar  string
}

// CurlInput holds the parsed URL and headers from a cURL command string.
type CurlInput struct {
	URL     string
	Headers map[string]string
}

// parseCurlCommand extracts the URL and headers from a cURL-like command string.
func parseCurlCommand(rawInput string) (CurlInput, error) {
	var result CurlInput
	result.Headers = make(map[string]string)

	// Regex to find the URL (typically the first argument, possibly quoted)
	// This regex tries to capture the content within the first pair of single or double quotes,
	// or the first non-flag argument if not quoted.
	urlRegex := regexp.MustCompile(`curl\s+(?:'([^']*)'|"([^"]*)"|(\S+))`)
	urlMatches := urlRegex.FindStringSubmatch(rawInput)
	if len(urlMatches) < 2 {
		return result, errors.New("无法解析 URL")
	}
	// The actual URL will be in one of the capturing groups
	for i := 1; i < len(urlMatches); i++ {
		if urlMatches[i] != "" {
			result.URL = urlMatches[i]
			break
		}
	}
	if result.URL == "" {
		return result, errors.New("未能从输入中提取 URL")
	}

	// Regex to find all headers (-H 'key: value' or -H "key: value")
	headerRegex := regexp.MustCompile(`-H\s+'([^']*)'|-H\s+"([^"]*)"`)
	headerMatches := headerRegex.FindAllStringSubmatch(rawInput, -1)

	for _, match := range headerMatches {
		// The actual header string is in the second or third capturing group
		headerStr := ""
		if len(match) > 1 && match[1] != "" {
			headerStr = match[1]
		} else if len(match) > 2 && match[2] != "" {
			headerStr = match[2]
		}

		if headerStr != "" {
			parts := strings.SplitN(headerStr, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				result.Headers[key] = value
			}
		}
	}

	// Add a default User-Agent if not provided, as some servers require it.
	if _, exists := result.Headers["User-Agent"]; !exists {
		result.Headers["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
	}

	return result, nil
}

// downloadFile downloads a file from a URL with custom headers and reports progress.
func downloadFile(url, fileName string, headers map[string]string, progressChan chan<- ProgressInfo) (string, error) {
	fmt.Printf("下载函数启动: URL=%s, FileName=%s\n", url, fileName)
	if fileName == "" {
		fileName = fmt.Sprintf("%s.m4s", randomName())
		fmt.Println("生成随机文件名:", fileName)
	}

	fmt.Println("准备 HTTP 请求...")
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("创建 HTTP 请求失败:", err)
		return "", err
	}

	// 设置请求头
	fmt.Println("设置请求头 (忽略 Range): ")
	for key, value := range headers {
		// 忽略 Range 请求头以确保下载完整文件
		if strings.EqualFold(key, "Range") {
			fmt.Printf("  忽略 Header: %s = %s\n", key, value)
			continue
		}
		fmt.Printf("  设置 Header: %s = %s\n", key, value)
		req.Header.Set(key, value)
	}

	fmt.Println("发起 HTTP 请求 (带超时)...")
	resp, err := httpClient.Do(req) // Use the client with timeout
	if err != nil {
		fmt.Println("HTTP 请求失败:", err)
		return "", err
	}
	defer resp.Body.Close()
	fmt.Println("HTTP 响应状态码:", resp.StatusCode)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent { // Allow Partial Content for range requests
		fmt.Printf("HTTP 错误! 状态码: %d\n", resp.StatusCode)
		return "", fmt.Errorf("HTTP error! status: %d", resp.StatusCode)
	}

	totalSize, _ := strconv.ParseInt(resp.Header.Get("content-length"), 10, 64)
	fmt.Printf("预期内容长度: %d 字节\n", totalSize)
	var downloadedSize int64 = 0

	// 用于记录过去一秒内的下载数据点
	downloadRecords := []downloadRecord{}

	fmt.Println("尝试创建本地文件:", fileName)
	file, err := os.Create(fileName)
	if err != nil {
		fmt.Println("创建文件失败:", err)
		return "", err
	}
	defer file.Close()
	fmt.Println("文件创建成功，开始读取响应体...")

	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			now := time.Now().UnixMilli()
			downloadedSize += int64(n)
			file.Write(buf[:n])

			// 记录当前下载点
			downloadRecords = append(downloadRecords, downloadRecord{
				timestamp: now,
				bytes:     n,
			})

			// 移除一秒前的记录
			oneSecondAgo := now - 1000
			for len(downloadRecords) > 0 && downloadRecords[0].timestamp < oneSecondAgo {
				downloadRecords = downloadRecords[1:]
			}

			// 计算过去一秒内的总下载量
			totalBytesInLastSecond := 0
			for _, record := range downloadRecords {
				totalBytesInLastSecond += record.bytes
			}

			// 计算时间跨度(秒)，使用最早和最新记录之间的时间差
			timeSpan := 1.0 // 默认1秒
			if len(downloadRecords) > 1 {
				timeSpan = float64(downloadRecords[len(downloadRecords)-1].timestamp-downloadRecords[0].timestamp) / 1000.0
				// 防止除以零或极小值
				if timeSpan < 0.1 {
					timeSpan = 0.1
				}
			}

			// 计算速度和进度
			speedPerSecond := float64(totalBytesInLastSecond) / timeSpan
			progress := float64(downloadedSize) / float64(totalSize) * 100
			progressBarWidth := 30
			filledWidth := int(math.Round((progress / 100) * float64(progressBarWidth)))
			progressBar := strings.Repeat("█", filledWidth) + strings.Repeat("░", progressBarWidth-filledWidth)
			speedInMB := speedPerSecond / 1024 / 1024
			downloadedMB := float64(downloadedSize) / 1024 / 1024
			totalMB := float64(totalSize) / 1024 / 1024

			// 发送进度信息到通道
			progressInfo := ProgressInfo{
				Percentage:   progress,
				DownloadedMB: downloadedMB,
				TotalMB:      totalMB,
				SpeedMBps:    speedInMB,
				ProgressBar:  progressBar,
			}
			progressChan <- progressInfo
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}

	// 关闭通道，表示下载完成
	close(progressChan)
	return fileName, nil
}

func main() {
	// 初始化随机数生成器
	rand.Seed(time.Now().UnixNano())

	// 获取输出文件名
	args := os.Args[1:]
	var movieName string
	if len(args) > 0 {
		movieName = filepath.Join(".", "download", args[0]+".mp4")
	} else {
		movieName = filepath.Join(".", "download", randomName()+".mp4")
	}

	// 用于从控制台读取输入
	reader := bufio.NewReader(os.Stdin)

	// Helper function to read multi-line input
	readMultiLineInput := func(prompt string) (string, error) {
		fmt.Println(prompt)
		var lines []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return "", err
			}
			trimmedLine := strings.TrimSpace(line)
			// Stop reading on empty line
			if trimmedLine == "" {
				break
			}
			// Remove trailing backslash if present (common in copied cURL commands)
			if strings.HasSuffix(trimmedLine, "\\") {
				trimmedLine = strings.TrimSuffix(trimmedLine, "\\")
				trimmedLine = strings.TrimSpace(trimmedLine) // Trim again after removing backslash
			}
			lines = append(lines, trimmedLine)
			if err == io.EOF {
				break // End if EOF is reached
			}
		}
		// Join lines with spaces to form a single command string
		return strings.Join(lines, " "), nil
	}

	// 获取视频 cURL 命令
	videoCurlInputStr, err := readMultiLineInput("请输入视频流的 cURL 命令 (输入空行结束):")
	if err != nil {
		fmt.Println("读取视频命令时出错:", err)
		os.Exit(1)
	}
	videoInput, err := parseCurlCommand(videoCurlInputStr)
	if err != nil {
		fmt.Println("解析视频 cURL 命令失败:", err)
		os.Exit(1)
	}

	// 获取音频 cURL 命令
	audioCurlInputStr, err := readMultiLineInput("请输入音频流的 cURL 命令 (输入空行结束):")
	if err != nil {
		fmt.Println("读取音频命令时出错:", err)
		os.Exit(1)
	}
	audioInput, err := parseCurlCommand(audioCurlInputStr)
	if err != nil {
		fmt.Println("解析音频 cURL 命令失败:", err)
		os.Exit(1)
	}

	try := func(f func() error) {
		if err := f(); err != nil {
			fmt.Println("执行出错:", err)
			os.Exit(1)
		}
	}

	try(func() error {
		createCache()

		videoProgressChan := make(chan ProgressInfo)
		audioProgressChan := make(chan ProgressInfo)

		var videoFilePath, audioFilePath string
		var videoErr, audioErr error

		fmt.Println("开始下载音频和视频...")

		// 启动视频下载goroutine
		fmt.Println("准备启动视频下载 goroutine...")
		go func() {
			fmt.Println("视频下载 goroutine 已启动")
			videoFilePath, videoErr = downloadFile(
				videoInput.URL,
				filepath.Join(".", ".cache", randomName()+".m4s"),
				videoInput.Headers,
				videoProgressChan,
			)
		}()

		// 启动音频下载goroutine
		fmt.Println("准备启动音频下载 goroutine...")
		go func() {
			fmt.Println("音频下载 goroutine 已启动")
			audioFilePath, audioErr = downloadFile(
				audioInput.URL,
				filepath.Join(".", ".cache", randomName()+".m4s"),
				audioInput.Headers,
				audioProgressChan,
			)
		}()

		// 跟踪最后一次打印的进度
		lastAudioProgress := ProgressInfo{}
		lastVideoProgress := ProgressInfo{}
		audioComplete := false
		videoComplete := false

		// 创建一个函数来打印进度
		printProgress := func() {
			// 使用回车符返回行首，然后清除当前行
			fmt.Print("\r\033[K")

			// 打印音频进度
			audioStatus := "音频: "
			if audioComplete {
				audioStatus += "下载完成!"
			} else if lastAudioProgress.ProgressBar != "" {
				audioStatus += fmt.Sprintf("[%s] %.2f%% | %.2fMB/%.2fMB | %.2f MB/s",
					lastAudioProgress.ProgressBar, lastAudioProgress.Percentage,
					lastAudioProgress.DownloadedMB, lastAudioProgress.TotalMB,
					lastAudioProgress.SpeedMBps)
			} else {
				audioStatus += "等待中..."
			}

			// 打印视频进度
			videoStatus := "视频: "
			if videoComplete {
				videoStatus += "下载完成!"
			} else if lastVideoProgress.ProgressBar != "" {
				videoStatus += fmt.Sprintf("[%s] %.2f%% | %.2fMB/%.2fMB | %.2f MB/s",
					lastVideoProgress.ProgressBar, lastVideoProgress.Percentage,
					lastVideoProgress.DownloadedMB, lastVideoProgress.TotalMB,
					lastVideoProgress.SpeedMBps)
			} else {
				videoStatus += "等待中..."
			}

			// 在一行内打印两者的状态，用足够的空格分隔
			fmt.Printf("%s | %s", audioStatus, videoStatus)
		}

		for !audioComplete || !videoComplete {
			select {
			case progress, ok := <-audioProgressChan:
				if ok {
					lastAudioProgress = progress
					printProgress()
				} else {
					audioProgressChan = nil
					audioComplete = true
					lastAudioProgress.Percentage = 100
					printProgress()
				}
			case progress, ok := <-videoProgressChan:
				if ok {
					lastVideoProgress = progress
					printProgress()
				} else {
					videoProgressChan = nil
					videoComplete = true
					lastVideoProgress.Percentage = 100
					printProgress()
				}
			}
		}

		fmt.Println("\n\n下载完成，开始合并音视频...")

		if videoErr != nil {
			return videoErr
		}

		if audioErr != nil {
			return audioErr
		}

		_, err := combineAudioAndVideo(videoFilePath, audioFilePath, movieName)
		if err != nil {
			return err
		}

		clearCache()
		return nil
	})
}
