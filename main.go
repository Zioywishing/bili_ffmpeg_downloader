package main

import (
	"bufio"
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

func createCache() {
	cacheDir := filepath.Join(".", ".cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		os.MkdirAll(cacheDir, 0755)
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

func downloadFile(url, fileName string) (string, error) {
	if fileName == "" {
		fileName = fmt.Sprintf("%s.m4s", randomName())
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	// 设置请求头
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6")
	req.Header.Set("sec-ch-ua", "\"Microsoft Edge\";v=\"131\", \"Chromium\";v=\"131\", \"Not_A Brand\";v=\"24\"")
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", "\"Windows\"")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "cross-site")
	req.Header.Set("Referer", "https://www.bilibili.com/video/BV1CqizYmEWG/?spm_id_from=333.1387.upload.video_card.click&vd_source=f4b11eff4d5b11ae41cb4e0ca94e674b")
	req.Header.Set("Referrer-Policy", "no-referrer-when-downgrade")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error! status: %d", resp.StatusCode)
	}

	totalSize, _ := strconv.ParseInt(resp.Header.Get("content-length"), 10, 64)
	var downloadedSize int64 = 0

	// 用于记录过去一秒内的下载数据点
	downloadRecords := []downloadRecord{}

	file, err := os.Create(fileName)
	if err != nil {
		return "", err
	}
	defer file.Close()

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
			progressBarWidth := 50
			filledWidth := int(math.Round((progress / 100) * float64(progressBarWidth)))
			progressBar := strings.Repeat("█", filledWidth) + strings.Repeat("░", progressBarWidth-filledWidth)
			speedInMB := speedPerSecond / 1024 / 1024
			downloadedMB := float64(downloadedSize) / 1024 / 1024
			totalMB := float64(totalSize) / 1024 / 1024

			fmt.Printf("\r下载进度: [%s] %.2f%% | %.2fMB/%.2fMB | 速度: %.2f MB/s",
				progressBar, progress, downloadedMB, totalMB, speedInMB)
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}

	fmt.Print("\r下载完成                                                                                           \n")
	return fileName, nil
}

func fetchAudio() string {
	return "https://xy182x201x240x114xy.mcdn.bilivideo.cn:8082/v1/resource/1661773258-1-30280.m4s?agrr=1&build=0&buvid=0646A41D-842F-4797-C129-303CFCA3C80966489infoc&bvc=vod&bw=94798&deadline=1743672270&dl=0&e=ig8euxZM2rNcNbdlhoNvNC8BqJIzNbfqXBvEqxTEto8BTrNvN0GvT90W5JZMkX_YN0MvXg8gNEV4NC8xNEV4N03eN0B5tZlqNxTEto8BTrNvNeZVuJ10Kj_g2UB02J0mN0B5tZlqNCNEto8BTrNvNC7MTX502C8f2jmMQJ6mqF2fka1mqx6gqj0eN0B599M%3D&f=u_0_0&gen=playurlv3&mcdnid=50022323&mid=40870994&nbs=1&nettype=0&og=hw&oi=1882261075&orderid=0%2C3&os=mcdn&platform=pc&sign=71d046&tag=&traceid=trAPOAGiYmSqIa_0_e_N&uipk=5&uparams=e%2Cnbs%2Coi%2Cgen%2Cos%2Cplatform%2Cmid%2Cdeadline%2Ctag%2Cog%2Ctrid%2Cuipk&upsig=538328095487b62e8dd868ad1934d536"
}

func fetchVideo() string {
	return "https://xy125x44x163x200xy.mcdn.bilivideo.cn:4483/upgcxcode/58/32/1661773258/1661773258-1-100029.m4s?e=ig8euxZM2rNcNbdlhoNvNC8BqJIzNbfqXBvEqxTEto8BTrNvN0GvT90W5JZMkX_YN0MvXg8gNEV4NC8xNEV4N03eN0B5tZlqNxTEto8BTrNvNeZVuJ10Kj_g2UB02J0mN0B5tZlqNCNEto8BTrNvNC7MTX502C8f2jmMQJ6mqF2fka1mqx6gqj0eN0B599M=&trid=0000700670a9d42e4656a34e6d177be5d31u&deadline=1743672270&nbs=1&uipk=5&gen=playurlv3&os=mcdn&oi=1882261075&platform=pc&mid=40870994&tag=&og=hw&upsig=a77f42745badf7e879db3e9ed1067032&uparams=e,trid,deadline,nbs,uipk,gen,os,oi,platform,mid,tag,og&mcdnid=50022323&bvc=vod&nettype=0&bw=4589659&f=u_0_0&agrr=1&buvid=0646A41D-842F-4797-C129-303CFCA3C80966489infoc&build=0&dl=0&orderid=0,3"
}

func main() {
	// 初始化随机数生成器
	rand.Seed(time.Now().UnixNano())

	args := os.Args[1:]
	var movieName string
	if len(args) > 0 {
		movieName = filepath.Join(".", "download", args[0]+".mp4")
	} else {
		movieName = filepath.Join(".", "download", randomName()+".mp4")
	}

	try := func(f func() error) {
		if err := f(); err != nil {
			fmt.Println("下载出错:", err)
			os.Exit(1)
		}
	}

	try(func() error {
		createCache()

		videoFilePath, err := downloadFile(fetchVideo(), filepath.Join(".", ".cache", randomName()+".m4s"))
		if err != nil {
			return err
		}

		audioFilePath, err := downloadFile(fetchAudio(), filepath.Join(".", ".cache", randomName()+".m4s"))
		if err != nil {
			return err
		}

		_, err = combineAudioAndVideo(videoFilePath, audioFilePath, movieName)
		if err != nil {
			return err
		}

		clearCache()
		return nil
	})
}
