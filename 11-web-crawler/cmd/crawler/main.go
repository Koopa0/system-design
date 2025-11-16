package main

import (
	"flag"
	"log"
	"time"

	"11-web-crawler/internal"
)

func main() {
	// 解析命令行參數
	workers := flag.Int("workers", 5, "Number of concurrent workers")
	maxDepth := flag.Int("max-depth", 3, "Maximum crawl depth")
	userAgent := flag.String("user-agent", "SimpleC rawler/1.0", "User-Agent string")
	seedURL := flag.String("seed", "https://example.com", "Seed URL to start crawling")
	flag.Parse()

	log.Println("🚀 Web Crawler Starting...")
	log.Printf("⚙️  Workers: %d", *workers)
	log.Printf("📊 Max Depth: %d", *maxDepth)
	log.Printf("🌐 Seed URL: %s", *seedURL)

	// 創建爬蟲配置
	config := &internal.Config{
		WorkerCount:   *workers,
		MaxDepth:      *maxDepth,
		UserAgent:     *userAgent,
		RespectRobots: true,
		CrawlDelay:    1 * time.Second,
		MaxURLs:       10000,
	}

	// 創建爬蟲
	crawler := internal.NewCrawler(config)

	// 設置處理器（打印爬取結果）
	crawler.SetHandler(func(url string, content []byte) {
		log.Printf("✅ Crawled: %s (size: %d bytes)", url, len(content))
		// 這裡可以添加自定義邏輯：
		// - 解析價格
		// - 提取鏈接
		// - 存入數據庫
	})

	// 添加種子 URL
	crawler.AddSeed(*seedURL, 0) // 優先級 0（最高）

	// 啟動爬取
	crawler.Start()

	log.Println("🎉 Crawler finished!")
	log.Printf("📈 Stats: %+v", crawler.GetStats())
}
