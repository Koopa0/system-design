# 多階段建構 - Go 應用最佳實踐
# 第一階段：建構
FROM golang:1.21-alpine AS builder

# 安裝建構依賴
RUN apk add --no-cache git make

# 設定工作目錄
WORKDIR /app

# 複製 go.mod 和 go.sum（利用 Docker 層快取）
COPY go.mod go.sum ./

# 下載依賴
RUN go mod download

# 複製源碼
COPY . .

# 建構執行檔
# CGO_ENABLED=0 產生靜態連結的執行檔
# -ldflags="-w -s" 減少執行檔大小
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s" \
    -o counter-service \
    cmd/server/main.go

# 第二階段：執行環境
FROM alpine:3.18

# 安裝必要的執行時依賴
RUN apk add --no-cache ca-certificates tzdata

# 設定時區為台北時間
ENV TZ=Asia/Taipei

# 創建非 root 用戶
RUN addgroup -g 1000 -S appuser && \
    adduser -u 1000 -S appuser -G appuser

# 設定工作目錄
WORKDIR /app

# 從建構階段複製執行檔
COPY --from=builder /app/counter-service .

# 複製配置檔案和 SQL
COPY --from=builder /app/config.yaml .
COPY --from=builder /app/sql ./sql

# 設定檔案權限
RUN chown -R appuser:appuser /app

# 切換到非 root 用戶
USER appuser

# 暴露服務端口
EXPOSE 8080

# 健康檢查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# 執行服務
ENTRYPOINT ["./counter-service"]