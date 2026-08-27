package ai

import (
	"sync"
)

// APIKeyManager 管理 API Key 的内存存储（不持久化，重启失效）
type APIKeyManager struct {
	apiKey string
	mutex  sync.RWMutex
}

// NewAPIKeyManager 创建新的 API Key 管理器
func NewAPIKeyManager() *APIKeyManager {
	return &APIKeyManager{}
}

// SetAPIKey 设置 API Key（覆盖之前的值）
func (m *APIKeyManager) SetAPIKey(key string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.apiKey = key
}

// GetAPIKey 获取 API Key
func (m *APIKeyManager) GetAPIKey() string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.apiKey
}

// HasAPIKey 检查是否设置了 API Key
func (m *APIKeyManager) HasAPIKey() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.apiKey != ""
}

// ClearAPIKey 清除 API Key
func (m *APIKeyManager) ClearAPIKey() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.apiKey = ""
}