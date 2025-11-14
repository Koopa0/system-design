#!/bin/bash
# 設置 Git Hooks

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "🔧 設置 Git Hooks..."

# 配置 Git 使用自定義 hooks 目錄
cd "$PROJECT_ROOT"
git config core.hooksPath .githooks

echo "✅ Git Hooks 設置完成"
echo ""
echo "現在 git commit 前會自動運行代碼檢查"
echo "跳過檢查：git commit --no-verify"
