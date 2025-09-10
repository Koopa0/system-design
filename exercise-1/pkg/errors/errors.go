// Package errors 提供應用程式錯誤處理
package errors

import (
	"errors"
	"fmt"
)

// 定義錯誤碼
const (
	// ErrCodeNotFound 資源未找到
	ErrCodeNotFound = "NOT_FOUND"
	// ErrCodeAlreadyExists 資源已存在
	ErrCodeAlreadyExists = "ALREADY_EXISTS"
	// ErrCodeInvalidInput 無效輸入
	ErrCodeInvalidInput = "INVALID_INPUT"
	// ErrCodeQuotaExceeded 配額超限
	ErrCodeQuotaExceeded = "QUOTA_EXCEEDED"
	// ErrCodeInternal 內部錯誤
	ErrCodeInternal = "INTERNAL_ERROR"
	// ErrCodeTimeout 超時錯誤
	ErrCodeTimeout = "TIMEOUT"
	// ErrCodeDegraded 降級模式
	ErrCodeDegraded = "SERVICE_DEGRADED"
	// ErrCodeUnavailable 服務不可用
	ErrCodeUnavailable = "SERVICE_UNAVAILABLE"
)

// AppError 應用程式錯誤
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
	Err     error  `json:"-"`
}

// Error 實現 error 介面
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap 實現 errors.Unwrap
func (e *AppError) Unwrap() error {
	return e.Err
}

// Is 實現 errors.Is
func (e *AppError) Is(target error) bool {
	t, ok := target.(*AppError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// New 創建新的應用程式錯誤
func New(code, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// Wrap 包裝錯誤
func Wrap(err error, code, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// WithDetails 添加詳細資訊
func (e *AppError) WithDetails(details string) *AppError {
	e.Details = details
	return e
}

// 預定義錯誤
var (
	// ErrCounterNotFound 計數器未找到
	ErrCounterNotFound = New(ErrCodeNotFound, "counter not found")
	
	// ErrCounterAlreadyExists 計數器已存在
	ErrCounterAlreadyExists = New(ErrCodeAlreadyExists, "counter already exists")
	
	// ErrInvalidCounterName 無效的計數器名稱
	ErrInvalidCounterName = New(ErrCodeInvalidInput, "invalid counter name")
	
	// ErrQuotaExceeded 配額超限
	ErrQuotaExceeded = New(ErrCodeQuotaExceeded, "counter quota exceeded")
	
	// ErrSystemCounterImmutable 系統計數器不可變更
	ErrSystemCounterImmutable = New(ErrCodeInvalidInput, "system counter cannot be deleted")
	
	// ErrServiceDegraded 服務降級
	ErrServiceDegraded = New(ErrCodeDegraded, "service is running in degraded mode")
	
	// ErrRedisUnavailable Redis 不可用
	ErrRedisUnavailable = New(ErrCodeUnavailable, "redis service unavailable")
	
	// ErrDatabaseUnavailable 資料庫不可用
	ErrDatabaseUnavailable = New(ErrCodeUnavailable, "database service unavailable")
)

// IsNotFound 檢查是否為未找到錯誤
func IsNotFound(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == ErrCodeNotFound
	}
	return false
}

// IsAlreadyExists 檢查是否為已存在錯誤
func IsAlreadyExists(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == ErrCodeAlreadyExists
	}
	return false
}

// IsQuotaExceeded 檢查是否為配額超限錯誤
func IsQuotaExceeded(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == ErrCodeQuotaExceeded
	}
	return false
}

// IsTimeout 檢查是否為超時錯誤
func IsTimeout(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == ErrCodeTimeout
	}
	return false
}

// IsDegraded 檢查是否為降級錯誤
func IsDegraded(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == ErrCodeDegraded
	}
	return false
}