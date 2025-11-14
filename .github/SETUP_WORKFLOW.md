# GitHub Actions Workflow 設置說明

由於權限限制，GitHub Actions workflow 文件無法通過 API 自動推送。

請按照以下步驟手動添加 workflow：

## 方式 1: 通過 GitHub UI（推薦）

1. 進入 GitHub 倉庫
2. 點擊 "Actions" 標籤
3. 點擊 "New workflow"
4. 選擇 "set up a workflow yourself"
5. 複製 `workflows/ci.yml` 的內容
6. 保存並提交

## 方式 2: 本地推送（需要權限）

```bash
# 如果您有 workflow 權限，可以直接推送
git add .github/workflows/ci.yml
git commit -m "ci: add GitHub Actions workflow"
git push
```

## Workflow 文件位置

```
.github/
└── workflows/
    └── ci.yml    # 主 CI 配置文件（已創建）
```

## Workflow 功能

該 workflow 包含 6 個並行 Jobs：

1. **Lint & Security** - 代碼檢查和安全掃描
2. **Unit Tests** - 單元測試（Go 1.23/1.24）
3. **SQL Verification** - sqlc 驗證
4. **Build** - 構建所有專案
5. **Dependency Check** - 依賴漏洞掃描
6. **Complexity** - 代碼複雜度分析

## 驗證設置

Workflow 添加後，您可以：

1. 推送代碼到任何分支觸發 CI
2. 在 "Actions" 標籤查看運行結果
3. 在 README 添加狀態徽章：

```markdown
[![CI](https://github.com/YOUR_ORG/system-design/actions/workflows/ci.yml/badge.svg)](https://github.com/YOUR_ORG/system-design/actions/workflows/ci.yml)
```

## 故障排除

如果 workflow 無法運行：

1. 確認倉庫設置中 Actions 已啟用
2. 檢查 workflow 文件語法（使用 YAML validator）
3. 查看 Actions 日誌獲取詳細錯誤

## 本地測試

在推送前，可以本地運行完整 CI：

```bash
make ci-local
```

這會運行所有 CI 檢查（除了 GitHub-specific 的部分）。
