package internal

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Config 爬蟲配置
type Config struct {
	WorkerCount   int
	MaxDepth      int
	UserAgent     string
	RespectRobots bool
	CrawlDelay    time.Duration
	MaxURLs       int
}

// Crawler 爬蟲
type Crawler struct {
	config      *Config
	bloomFilter *BloomFilter
	robotsCache map[string]*RobotsTxt
	urlQueue    chan *URLItem
	wg          sync.WaitGroup
	handler     func(url string, content []byte)
	stats       *Stats
	mu          sync.RWMutex
}

// URLItem URL 項目
type URLItem struct {
	URL      string
	Priority int
	Depth    int
}

// Stats 統計數據
type Stats struct {
	URLsProcessed int
	URLsDiscovered int
	Errors        int
	StartTime     time.Time
	EndTime       time.Time
}

// NewCrawler 創建爬蟲
func NewCrawler(config *Config) *Crawler {
	return &Crawler{
		config:      config,
		bloomFilter: NewBloomFilter(config.MaxURLs, 0.01),
		robotsCache: make(map[string]*RobotsTxt),
		urlQueue:    make(chan *URLItem, 1000),
		handler:     nil,
		stats: &Stats{
			StartTime: time.Now(),
		},
	}
}

// SetHandler 設置處理器
func (c *Crawler) SetHandler(handler func(url string, content []byte)) {
	c.handler = handler
}

// AddSeed 添加種子 URL
func (c *Crawler) AddSeed(urlStr string, priority int) {
	if c.bloomFilter.Contains(urlStr) {
		return
	}

	c.bloomFilter.Add(urlStr)
	c.urlQueue <- &URLItem{
		URL:      urlStr,
		Priority: priority,
		Depth:    0,
	}

	c.mu.Lock()
	c.stats.URLsDiscovered++
	c.mu.Unlock()
}

// Start 啟動爬蟲
func (c *Crawler) Start() {
	// 啟動 worker
	for i := 0; i < c.config.WorkerCount; i++ {
		c.wg.Add(1)
		go c.worker(i)
	}

	// 等待所有 worker 完成
	c.wg.Wait()
	close(c.urlQueue)

	c.stats.EndTime = time.Now()
}

// worker 工作協程
func (c *Crawler) worker(id int) {
	defer c.wg.Done()

	for item := range c.urlQueue {
		// 檢查深度
		if item.Depth > c.config.MaxDepth {
			continue
		}

		// 爬取
		if err := c.crawl(item); err != nil {
			log.Printf("Worker %d: Error crawling %s: %v", id, item.URL, err)
			c.mu.Lock()
			c.stats.Errors++
			c.mu.Unlock()
		}

		// 禮貌性：等待一段時間
		time.Sleep(c.config.CrawlDelay)
	}
}

// crawl 爬取單個 URL
func (c *Crawler) crawl(item *URLItem) error {
	parsedURL, err := url.Parse(item.URL)
	if err != nil {
		return err
	}

	domain := parsedURL.Scheme + "://" + parsedURL.Host

	// 檢查 robots.txt
	if c.config.RespectRobots {
		c.mu.Lock()
		robotsTxt := c.robotsCache[domain]
		if robotsTxt == nil {
			robotsTxt = NewRobotsTxt()
			robotsTxt.Fetch(domain)
			c.robotsCache[domain] = robotsTxt
		}
		c.mu.Unlock()

		if !robotsTxt.IsAllowed(c.config.UserAgent, parsedURL.Path) {
			log.Printf("Disallowed by robots.txt: %s", item.URL)
			return nil
		}

		// 遵守 Crawl-delay
		delay := robotsTxt.GetCrawlDelay(c.config.UserAgent)
		if delay > c.config.CrawlDelay {
			time.Sleep(delay - c.config.CrawlDelay)
		}
	}

	// 發送 HTTP 請求
	req, err := http.NewRequest("GET", item.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.config.UserAgent)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 只處理成功的響應
	if resp.StatusCode != 200 {
		return nil
	}

	// 讀取內容
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 調用處理器
	if c.handler != nil {
		c.handler(item.URL, content)
	}

	// 更新統計
	c.mu.Lock()
	c.stats.URLsProcessed++
	c.mu.Unlock()

	// 這裡可以提取新的 URL 並添加到隊列
	// （簡化版本省略）

	return nil
}

// GetStats 獲取統計數據
func (c *Crawler) GetStats() *Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := *c.stats
	return &stats
}
