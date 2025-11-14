# 開發指南

> **快速開始**：設置開發環境和工作流程

---

## 🚀 快速開始

### 1. 克隆倉庫

```bash
git clone https://github.com/YOUR_ORG/system-design.git
cd system-design
```

### 2. 安裝工具

```bash
# 安裝所有開發工具
make install-tools

# 包括：
# - golangci-lint（代碼檢查）
# - gosec（安全掃描）
# - sqlc（SQL 代碼生成）
# - govulncheck（漏洞掃描）
# - gocyclo（複雜度分析）
```

### 3. 設置 Git Hooks

```bash
# 自動在 commit 前檢查代碼
./scripts/setup-hooks.sh
```

### 4. 驗證設置

```bash
# 運行快速 CI 檢查
make ci-quick
```

---

## 📝 日常開發流程

### 開發新功能

```bash
# 1. 創建新分支
git checkout -b feature/my-feature

# 2. 編寫代碼
# ...

# 3. 格式化代碼
make fmt

# 4. 運行測試
make test

# 5. 提交（自動檢查）
git add .
git commit -m "feat: add new feature"

# 6. 推送
git push origin feature/my-feature
```

### 修復 Bug

```bash
# 1. 創建修復分支
git checkout -b fix/issue-123

# 2. 修復並添加測試
# ...

# 3. 驗證修復
make test

# 4. 提交
git commit -m "fix: resolve issue #123"
```

### 重構代碼

```bash
# 1. 確保測試通過
make test

# 2. 重構代碼
# ...

# 3. 運行完整檢查
make ci-local

# 4. 提交
git commit -m "refactor: improve code structure"
```

---

## 🔍 常用命令

### 代碼質量

```bash
make lint              # 檢查代碼
make lint-fix          # 自動修復問題
make fmt               # 格式化代碼
make security          # 安全掃描
make complexity        # 複雜度分析
```

### 測試

```bash
make test              # 單元測試
make test-coverage     # 測試覆蓋率
make test-short        # 快速測試
```

### 構建

```bash
make build             # 構建所有專案
make build-counter     # 構建單個專案
```

### CI

```bash
make pre-commit        # 提交前檢查（快速）
make ci-quick          # 快速 CI
make ci-local          # 完整 CI
```

---

## 🐳 Docker 開發環境

### 啟動服務

```bash
# 啟動 PostgreSQL + Redis
make docker-up

# 檢查服務狀態
docker-compose ps
```

### 運行專案

```bash
# 以 Counter Service 為例
cd 01-counter-service

# 運行遷移
make migrate-up

# 啟動服務
go run cmd/server/main.go
```

### 停止服務

```bash
make docker-down
```

---

## 🧪 測試策略

### 單元測試

```bash
# 運行所有測試
go test ./...

# 運行特定包
go test ./internal/counter

# Verbose 模式
go test -v ./...

# Race detector
go test -race ./...
```

### 集成測試

```bash
# 1. 啟動依賴服務
make docker-up

# 2. 設置環境變量
export DATABASE_URL=postgres://localhost:5432/testdb
export REDIS_URL=redis://localhost:6379

# 3. 運行測試
go test ./...
```

### 測試覆蓋率

```bash
# 生成覆蓋率報告
make test-coverage

# 查看 HTML 報告
open coverage.html
```

---

## 📊 代碼質量標準

### 必須通過的檢查

✅ **格式化**: `gofmt` 無問題
✅ **Linting**: `golangci-lint` 通過
✅ **測試**: 所有測試通過
✅ **構建**: 成功構建
✅ **安全**: 無安全漏洞

### 代碼規範

- **函數長度**: ≤ 100 行
- **圈複雜度**: ≤ 15
- **測試覆蓋率**: ≥ 70%
- **註解**: 公開 API 必須有文檔

---

## 🔧 故障排除

### Pre-commit 失敗

```bash
# 查看具體錯誤
git commit

# 修復格式問題
make fmt

# 修復 lint 問題
make lint-fix

# 暫時跳過（不推薦）
git commit --no-verify
```

### 測試失敗

```bash
# 查看詳細日誌
go test -v ./...

# 運行單個測試
go test -run TestFunctionName ./...

# 調試模式
go test -v -run TestFunctionName ./...
```

### Docker 服務問題

```bash
# 重啟服務
make docker-down
make docker-up

# 查看日誌
docker-compose logs postgres
docker-compose logs redis

# 清理並重啟
docker-compose down -v
make docker-up
```

---

## 📚 推薦閱讀

- [CI/CD 配置詳解](./CI_CD.md)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [golangci-lint Linters](https://golangci-lint.run/usage/linters/)

---

## 💡 小技巧

### 快速修復常見問題

```bash
# 格式化所有代碼
make fmt

# 整理 imports
goimports -w .

# 更新依賴
go get -u ./...
go mod tidy
```

### IDE 集成

**VS Code**: 安裝 Go 擴展並配置：

```json
{
  "go.lintTool": "golangci-lint",
  "go.lintFlags": ["--fast"],
  "editor.formatOnSave": true
}
```

**GoLand**: Settings → Go → Golangci-Lint

### 提高開發效率

```bash
# 僅檢查修改的文件
golangci-lint run --new

# 並行測試
go test -parallel=4 ./...

# 使用 watch 模式
ls **/*.go | entr -c go test ./...
```

---

**需要幫助？**

- 查看 [CI/CD 文檔](./CI_CD.md)
- 運行 `make help` 查看所有命令
- 提交 Issue 到 GitHub
