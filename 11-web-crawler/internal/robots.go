package internal

import (
	"bufio"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RobotsTxt robots.txt 解析器
type RobotsTxt struct {
	rules map[string]*Rules // 按 User-Agent 分組
	mu    sync.RWMutex
}

// Rules robots.txt 規則
type Rules struct {
	Disallow   []string
	Allow      []string
	CrawlDelay time.Duration
}

// NewRobotsTxt 創建 robots.txt 解析器
func NewRobotsTxt() *RobotsTxt {
	return &RobotsTxt{
		rules: make(map[string]*Rules),
	}
}

// Fetch 獲取並解析 robots.txt
func (r *RobotsTxt) Fetch(baseURL string) error {
	robotsURL := baseURL + "/robots.txt"

	resp, err := http.Get(robotsURL)
	if err != nil {
		// robots.txt 不存在，默認允許所有
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil // robots.txt 不存在
	}

	// 解析 robots.txt
	scanner := bufio.NewScanner(resp.Body)
	var currentAgent string
	currentRules := &Rules{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳過註釋和空行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			if currentAgent != "" {
				r.mu.Lock()
				r.rules[currentAgent] = currentRules
				r.mu.Unlock()
			}
			currentAgent = value
			currentRules = &Rules{}

		case "disallow":
			if value != "" {
				currentRules.Disallow = append(currentRules.Disallow, value)
			}

		case "allow":
			currentRules.Allow = append(currentRules.Allow, value)

		case "crawl-delay":
			if delay, err := time.ParseDuration(value + "s"); err == nil {
				currentRules.CrawlDelay = delay
			}
		}
	}

	// 保存最後一個 User-Agent 的規則
	if currentAgent != "" {
		r.mu.Lock()
		r.rules[currentAgent] = currentRules
		r.mu.Unlock()
	}

	return nil
}

// IsAllowed 檢查 URL 是否允許爬取
func (r *RobotsTxt) IsAllowed(userAgent, path string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 查找規則（先查具體 User-Agent，再查 *）
	rules := r.rules[userAgent]
	if rules == nil {
		rules = r.rules["*"]
	}
	if rules == nil {
		return true // 沒有規則，默認允許
	}

	// 檢查 Allow 規則（優先級高）
	for _, allow := range rules.Allow {
		if strings.HasPrefix(path, allow) {
			return true
		}
	}

	// 檢查 Disallow 規則
	for _, disallow := range rules.Disallow {
		if strings.HasPrefix(path, disallow) {
			return false
		}
	}

	return true // 沒有匹配的規則，默認允許
}

// GetCrawlDelay 獲取 Crawl-delay
func (r *RobotsTxt) GetCrawlDelay(userAgent string) time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules := r.rules[userAgent]
	if rules == nil {
		rules = r.rules["*"]
	}
	if rules == nil {
		return 0
	}

	return rules.CrawlDelay
}
