# GitHub Actions 設置

## 啟用 CI Workflow

```bash
# 複製 workflow 文件
mkdir -p .github/workflows
cp docs/ci.yml.example .github/workflows/ci.yml

# 提交並推送
git add .github/workflows/ci.yml
git commit -m "ci: add GitHub Actions workflow"
git push
```

## Required Status Checks

Workflow 已配置以下 jobs，匹配 GitHub rule set 要求：

- **lint** - 代碼質量與安全檢查
- **test** - 單元測試（Go 1.23/1.24 + PostgreSQL + Redis）
- **build** - 構建所有專案

## 本地測試

```bash
make ci-local  # 完整 CI 檢查
make ci-quick  # 快速檢查
```
