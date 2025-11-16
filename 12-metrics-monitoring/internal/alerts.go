package internal

import (
	"fmt"
	"sync"
	"time"
)

// AlertRule 告警規則
type AlertRule struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	MetricName  string        `json:"metric_name"`
	Condition   string        `json:"condition"`   // ">", "<", "==", ">=", "<="
	Threshold   float64       `json:"threshold"`
	Duration    time.Duration `json:"duration"`    // 持續時間
	Severity    string        `json:"severity"`    // "info", "warning", "critical"
	Description string        `json:"description"`
	Enabled     bool          `json:"enabled"`
}

// Alert 告警
type Alert struct {
	RuleName     string    `json:"rule_name"`
	MetricName   string    `json:"metric_name"`
	CurrentValue float64   `json:"current_value"`
	Threshold    float64   `json:"threshold"`
	StartedAt    time.Time `json:"started_at"`
	Duration     string    `json:"duration"`
	Severity     string    `json:"severity"`
	Description  string    `json:"description"`
}

// AlertState 告警狀態
type AlertState struct {
	RuleID      string
	Active      bool
	StartedAt   time.Time
	LastChecked time.Time
	LastValue   float64
}

// AlertEngine 告警引擎
type AlertEngine struct {
	config   *Config
	db       *TimeSeriesDB
	rules    map[string]*AlertRule
	states   map[string]*AlertState
	callback func(*Alert)
	mu       sync.RWMutex
	stopChan chan bool
}

// NewAlertEngine 創建告警引擎
func NewAlertEngine(config *Config, db *TimeSeriesDB) *AlertEngine {
	return &AlertEngine{
		config:   config,
		db:       db,
		rules:    make(map[string]*AlertRule),
		states:   make(map[string]*AlertState),
		stopChan: make(chan bool),
	}
}

// AddRule 添加告警規則
func (ae *AlertEngine) AddRule(rule *AlertRule) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule-%d", time.Now().UnixNano())
	}

	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}

	if rule.MetricName == "" {
		return fmt.Errorf("metric name is required")
	}

	if rule.Condition == "" {
		return fmt.Errorf("condition is required")
	}

	ae.rules[rule.ID] = rule
	ae.states[rule.ID] = &AlertState{
		RuleID: rule.ID,
		Active: false,
	}

	return nil
}

// RemoveRule 刪除告警規則
func (ae *AlertEngine) RemoveRule(ruleID string) error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	delete(ae.rules, ruleID)
	delete(ae.states, ruleID)

	return nil
}

// GetRules 獲取所有告警規則
func (ae *AlertEngine) GetRules() []*AlertRule {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	rules := make([]*AlertRule, 0, len(ae.rules))
	for _, rule := range ae.rules {
		rules = append(rules, rule)
	}

	return rules
}

// GetActiveAlerts 獲取當前活動的告警
func (ae *AlertEngine) GetActiveAlerts() []*Alert {
	ae.mu.RLock()
	defer ae.mu.RUnlock()

	alerts := make([]*Alert, 0)

	for ruleID, state := range ae.states {
		if !state.Active {
			continue
		}

		rule, exists := ae.rules[ruleID]
		if !exists {
			continue
		}

		duration := time.Since(state.StartedAt)
		alerts = append(alerts, &Alert{
			RuleName:     rule.Name,
			MetricName:   rule.MetricName,
			CurrentValue: state.LastValue,
			Threshold:    rule.Threshold,
			StartedAt:    state.StartedAt,
			Duration:     duration.String(),
			Severity:     rule.Severity,
			Description:  rule.Description,
		})
	}

	return alerts
}

// SetCallback 設置告警回調函數
func (ae *AlertEngine) SetCallback(callback func(*Alert)) {
	ae.callback = callback
}

// Start 啟動告警引擎
func (ae *AlertEngine) Start() {
	ticker := time.NewTicker(ae.config.AlertEvalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ae.evaluate()
		case <-ae.stopChan:
			return
		}
	}
}

// Stop 停止告警引擎
func (ae *AlertEngine) Stop() {
	ae.stopChan <- true
}

// evaluate 評估所有告警規則
func (ae *AlertEngine) evaluate() {
	ae.mu.RLock()
	rules := make([]*AlertRule, 0, len(ae.rules))
	for _, rule := range ae.rules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	ae.mu.RUnlock()

	now := time.Now()

	for _, rule := range rules {
		ae.evaluateRule(rule, now)
	}
}

// evaluateRule 評估單個告警規則
func (ae *AlertEngine) evaluateRule(rule *AlertRule, now time.Time) {
	// 查詢最近的數據
	end := now.Unix()
	start := end - int64(rule.Duration.Seconds())

	metrics := ae.db.QueryRange(rule.MetricName, start, end, nil)

	if len(metrics) == 0 {
		return
	}

	// 計算平均值
	var sum float64
	for _, m := range metrics {
		sum += m.Value
	}
	avgValue := sum / float64(len(metrics))

	// 檢查條件
	triggered := ae.checkCondition(avgValue, rule.Condition, rule.Threshold)

	ae.mu.Lock()
	defer ae.mu.Unlock()

	state := ae.states[rule.ID]
	state.LastChecked = now
	state.LastValue = avgValue

	if triggered {
		if !state.Active {
			// 新觸發的告警
			state.Active = true
			state.StartedAt = now

			// 觸發回調
			if ae.callback != nil {
				alert := &Alert{
					RuleName:     rule.Name,
					MetricName:   rule.MetricName,
					CurrentValue: avgValue,
					Threshold:    rule.Threshold,
					StartedAt:    now,
					Duration:     "0s",
					Severity:     rule.Severity,
					Description:  rule.Description,
				}
				go ae.callback(alert)
			}
		} else {
			// 持續觸發的告警
			duration := now.Sub(state.StartedAt)
			if duration >= rule.Duration {
				// 持續時間超過閾值，發送告警
				if ae.callback != nil {
					alert := &Alert{
						RuleName:     rule.Name,
						MetricName:   rule.MetricName,
						CurrentValue: avgValue,
						Threshold:    rule.Threshold,
						StartedAt:    state.StartedAt,
						Duration:     duration.String(),
						Severity:     rule.Severity,
						Description:  rule.Description,
					}
					go ae.callback(alert)
				}
			}
		}
	} else {
		// 告警恢復
		if state.Active {
			state.Active = false
		}
	}
}

// checkCondition 檢查條件
func (ae *AlertEngine) checkCondition(value float64, condition string, threshold float64) bool {
	switch condition {
	case ">":
		return value > threshold
	case "<":
		return value < threshold
	case "==":
		return value == threshold
	case ">=":
		return value >= threshold
	case "<=":
		return value <= threshold
	default:
		return false
	}
}

// EvaluateRuleNow 立即評估某個規則（用於測試）
func (ae *AlertEngine) EvaluateRuleNow(ruleID string) (*Alert, error) {
	ae.mu.RLock()
	rule, exists := ae.rules[ruleID]
	ae.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("rule not found: %s", ruleID)
	}

	now := time.Now()
	end := now.Unix()
	start := end - int64(rule.Duration.Seconds())

	metrics := ae.db.QueryRange(rule.MetricName, start, end, nil)

	if len(metrics) == 0 {
		return nil, fmt.Errorf("no metrics found")
	}

	// 計算平均值
	var sum float64
	for _, m := range metrics {
		sum += m.Value
	}
	avgValue := sum / float64(len(metrics))

	// 檢查條件
	triggered := ae.checkCondition(avgValue, rule.Condition, rule.Threshold)

	if triggered {
		return &Alert{
			RuleName:     rule.Name,
			MetricName:   rule.MetricName,
			CurrentValue: avgValue,
			Threshold:    rule.Threshold,
			StartedAt:    now,
			Duration:     "0s",
			Severity:     rule.Severity,
			Description:  rule.Description,
		}, nil
	}

	return nil, nil
}
