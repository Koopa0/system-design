# CI/CD 配置與最佳實踐

> **目標**：確保代碼質量、安全性和可靠性
>
> **原則**：Shift-Left（盡早發現問題）、快速反饋、自動化一切

---

## 📋 目錄

- [CI 流程概覽](#ci-流程概覽)
- [本地開發工作流](#本地開發工作流)
- [GitHub Actions 配置](#github-actions-配置)
- [代碼質量檢查](#代碼質量檢查)
- [安全掃描](#安全掃描)
- [故障排除](#故障排除)

---

## CI 流程概覽

### 檢查項目

```
┌─────────────────────────────────────────────────────────┐
│                   CI Pipeline                            │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  1. 代碼質量 (Lint & Security)                           │
│     ├─ golangci-lint (20+ linters)                      │
│     ├─ gofmt (格式檢查)                                   │
│     ├─ gosec (安全掃描)                                   │
│     └─ go mod verify                                     │
│                                                          │
│  2. 單元測試 (Test)                                       │
│     ├─ Go 1.23 / 1.24 (多版本)                           │
│     ├─ Race detector                                     │
│     ├─ Coverage report                                   │
│     └─ PostgreSQL + Redis (服務)                         │
│                                                          │
│  3. SQL 驗證 (sqlc)                                       │
│     └─ 確保生成的代碼是最新的                               │
│                                                          │
│  4. 構建驗證 (Build)                                      │
│     ├─ 01-counter-service                               │
│     ├─ 02-room-management                               │
│     └─ 03-url-shortener                                 │
│                                                          │
│  5. 依賴安全 (Dependency)                                 │
│     └─ govulncheck (漏洞掃描)                            │
│                                                          │
│  6. 代碼複雜度 (Complexity)                               │
│     └─ gocyclo (圈複雜度)                                │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### 運行時間估算

| 階段 | 時間 | 並行 |
|------|------|------|
| Lint & Security | ~2 分鐘 | ✓ |
| Unit Tests (2 版本) | ~3 分鐘 | ✓ |
| SQL Verify | ~30 秒 | ✓ |
| Build (3 專案) | ~2 分鐘 | ✓ |
| Dependency Check | ~1 分鐘 | ✓ |
| **總計** | **~3-4 分鐘** | |

---

## 本地開發工作流

### 初始設置

```bash
# 1. 安裝開發工具
make install-tools

# 2. 設置 Git Hooks（自動檢查）
./scripts/setup-hooks.sh
```

### 日常開發

```bash
# 開發前：拉取最新代碼
git pull

# 開發中：隨時格式化
make fmt

# 提交前：運行快速檢查
make pre-commit

# 或手動檢查各項
make fmt-check    # 格式檢查
make lint         # 代碼檢查
make test-short   # 快速測試
```

### 提交流程

```bash
# 方式 1：自動檢查（推薦）
git add .
git commit -m "feat: xxx"
# → 自動運行 pre-commit hook

# 方式 2：跳過檢查（緊急情況）
git commit --no-verify -m "wip: xxx"
# ⚠️ 不推薦，CI 仍會檢查
```

### 完整本地 CI

```bash
# 運行完整 CI 流程（推送前）
make ci-local

# 或快速版本
make ci-quick
```

---

## GitHub Actions 配置

### 文件位置

```
.github/
└── workflows/
    └── ci.yml      # 主 CI 配置
```

### 觸發條件

- ✅ **Push**: 任何分支推送時觸發
- ✅ **Pull Request**: PR 到 main 時觸發
- ✅ **手動觸發**: GitHub UI 手動運行

### 環境變量

CI 自動提供以下服務：

```yaml
# PostgreSQL
POSTGRES_USER: postgres
POSTGRES_PASSWORD: postgres
POSTGRES_DB: testdb
PORT: 5432

# Redis
PORT: 6379
```

### 狀態徽章

在 README 中添加：

```markdown
[![CI Status](https://github.com/YOUR_ORG/system-design/actions/workflows/ci.yml/badge.svg)](https://github.com/YOUR_ORG/system-design/actions/workflows/ci.yml)
```

---

## 代碼質量檢查

### golangci-lint

**配置**: `.golangci.yml`

**啟用的 Linter** (20+):

| 類別 | Linter | 說明 |
|------|--------|------|
| **基礎** | errcheck | 未處理的錯誤 |
| | staticcheck | 靜態分析（重要） |
| | govet | Go 官方工具 |
| **風格** | gofmt | 格式化 |
| | goimports | Import 整理 |
| | misspell | 拼寫錯誤 |
| **性能** | prealloc | Slice 預分配 |
| | unconvert | 不必要的轉換 |
| **安全** | gosec | 安全漏洞（G1xx） |
| **複雜度** | gocyclo | 圈複雜度 (≤15) |
| | funlen | 函數長度 (≤100 行) |
| **並發** | rowserrcheck | SQL rows.Err() |
| | sqlclosecheck | SQL Close() |

**運行方式**:

```bash
# 檢查所有代碼
make lint

# 自動修復
make lint-fix

# 僅檢查新代碼（快速）
golangci-lint run --new-from-rev=origin/main
```

### 格式化

```bash
# 自動格式化
make fmt

# 僅檢查（CI 用）
make fmt-check
```

---

## 安全掃描

### gosec - 代碼安全

**檢查項目**:

- G101: 硬編碼密碼
- G104: 未檢查的錯誤
- G201/G202: SQL 注入
- G301-G306: 文件權限
- G401-G405: 弱加密算法

**運行**:

```bash
make security

# 僅 gosec
gosec ./...

# 生成報告
gosec -fmt=json -out=report.json ./...
```

### govulncheck - 依賴漏洞

**檢查**:

- Go 標準庫漏洞
- 第三方依賴漏洞
- 間接依賴漏洞

**運行**:

```bash
make vuln

# 或直接
govulncheck ./...
```

---

## 測試策略

### 測試類型

```bash
# 單元測試（快速）
go test ./...

# 包含 race detector
go test -race ./...

# 短測試（跳過慢速測試）
go test -short ./...

# 覆蓋率
make test-coverage
# → 生成 coverage.html
```

### 集成測試

需要 Docker 服務：

```bash
# 啟動服務
make docker-up

# 運行測試
DATABASE_URL=postgres://localhost:5432/testdb \
REDIS_URL=redis://localhost:6379 \
go test ./...

# 停止服務
make docker-down
```

### CI 中的測試

GitHub Actions 自動提供：

- PostgreSQL 16
- Redis 7
- 健康檢查（確保服務就緒）

---

## 構建驗證

### 本地構建

```bash
# 構建所有專案
make build

# 構建單個專案
make build-counter
make build-room
make build-url
```

### CI 構建

CI 會對每個專案執行：

```bash
go build -v -o /tmp/app ./cmd/server/main.go
```

---

## Makefile 命令參考

### 常用命令

```bash
make help              # 顯示所有命令
make install-tools     # 安裝開發工具
make pre-commit        # 提交前檢查（快速）
make ci-local          # 完整 CI（本地）
make ci-quick          # 快速 CI
```

### 檢查命令

```bash
make lint              # 代碼檢查
make lint-fix          # 自動修復
make fmt               # 格式化
make fmt-check         # 格式檢查
make security          # 安全掃描
make vuln              # 漏洞掃描
make complexity        # 複雜度分析
```

### 測試命令

```bash
make test              # 單元測試
make test-coverage     # 測試覆蓋率
make test-short        # 快速測試
```

### 構建命令

```bash
make build             # 構建所有
make build-counter     # 構建 Counter
make build-room        # 構建 Room
make build-url         # 構建 URL
```

### 依賴命令

```bash
make tidy              # 整理 modules
make verify            # 驗證 modules
make download          # 下載依賴
```

---

## 故障排除

### 常見問題

#### 1. golangci-lint 超時

**問題**: `deadline exceeded`

**解決**:

```bash
# 增加超時時間
golangci-lint run --timeout=10m

# 或減少檢查範圍
golangci-lint run --new-from-rev=HEAD~1
```

#### 2. 格式化失敗

**問題**: `gofmt` 報告格式問題

**解決**:

```bash
# 自動修復
make fmt

# 或手動
gofmt -w -s .
```

#### 3. 測試需要服務

**問題**: 測試失敗因為缺少 PostgreSQL/Redis

**解決**:

```bash
# 啟動 Docker 服務
make docker-up

# 或設置環境變量跳過集成測試
go test -short ./...
```

#### 4. Pre-commit Hook 失敗

**問題**: Commit 被阻止

**解決**:

```bash
# 選項 1：修復問題
make lint-fix
make fmt

# 選項 2：暫時跳過（不推薦）
git commit --no-verify

# 選項 3：禁用 hook
git config core.hooksPath ""
```

### 日誌查看

**GitHub Actions**:

1. 進入 GitHub 倉庫
2. 點擊 "Actions" 標籤
3. 選擇失敗的 workflow
4. 查看具體 job 的日誌

**本地日誌**:

```bash
# Verbose 模式
make lint VERBOSE=1
go test -v ./...
```

---

## 最佳實踐

### ✅ DO

- **提交前**: 運行 `make pre-commit`
- **格式化**: 隨時運行 `make fmt`
- **小提交**: 頻繁提交，小批量修改
- **測試**: 新代碼必須有測試
- **註解**: 複雜邏輯添加註解

### ❌ DON'T

- **跳過檢查**: 避免 `--no-verify`
- **大量修改**: 一次修改太多文件
- **忽略警告**: Linter 警告也要處理
- **未測試**: 不寫測試就提交
- **硬編碼**: 避免硬編碼密碼/URL

---

## 性能優化建議

### 加速本地檢查

```bash
# 1. 僅檢查修改的文件
golangci-lint run --new

# 2. 使用快速測試
go test -short ./...

# 3. 並行運行
go test -parallel=4 ./...

# 4. 跳過 vendor
golangci-lint run --skip-dirs=vendor
```

### 加速 CI

- ✅ 使用 Go cache (`cache: true`)
- ✅ 並行運行 jobs
- ✅ 只在必要時運行測試
- ✅ 使用 Docker layer cache

---

## 持續改進

### 定期檢查

- [ ] 每月更新 golangci-lint
- [ ] 每季度檢查新的 linter
- [ ] 定期運行 `govulncheck`
- [ ] 監控 CI 運行時間

### 監控指標

- **CI 通過率**: 目標 >95%
- **平均運行時間**: 目標 <5 分鐘
- **測試覆蓋率**: 目標 >70%
- **安全漏洞**: 目標 = 0

---

**相關文檔**:

- [golangci-lint 配置](./.golangci.yml)
- [GitHub Actions 配置](../.github/workflows/ci.yml)
- [Makefile](../Makefile)
- [Pre-commit Hook](../.githooks/pre-commit)
