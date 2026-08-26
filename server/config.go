package main

import (
	"os"
	"strconv"
	"time"
)

// Config 为环境变量配置骨架（PRD §4.5 / §4.1）。
// 敏感项（密码/AI key）仅环境变量读取，不入库、不进设置界面、不落日志。
type Config struct {
	Host             string        // HOST，监听地址，默认 127.0.0.1（仅本机；局域网访问设 0.0.0.0）
	Port             string        // PORT，监听端口，默认 8081
	DBPath           string        // DB_PATH，SQLite 文件路径，默认 data.db
	Password         string        // STUDIO_PASSWORD，缺失时拒绝启动
	AuthFailCooldown time.Duration // AUTH_FAIL_COOLDOWN，登录失败冷却秒数，默认 30
	CookieSecure     bool          // COOKIE_SECURE，https 部署时开启（cookie 带 Secure）
	TrustXFF         bool          // TRUST_XFF，nginx 反代部署时开启（冷却按 X-Forwarded-For）
	AIProvider       string        // AI_PROVIDER，默认 deepseek
	AIAPIKey         string        // AI_API_KEY，缺失时 AI 功能返回明确错误
	AIModel          string        // AI_MODEL
	AIBaseURL        string        // AI_BASE_URL，默认 DeepSeek 官方
}

// LoadConfig 启动时读取一次；后续如需热更新另行设计。
func LoadConfig() Config {
	return Config{
		Host:             getenv("HOST", "127.0.0.1"),
		Port:             getenv("PORT", "8081"),
		DBPath:           getenv("DB_PATH", "data.db"),
		Password:         os.Getenv("STUDIO_PASSWORD"),
		AuthFailCooldown: cooldownFromEnv(),
		CookieSecure:     envBool("COOKIE_SECURE"),
		TrustXFF:         envBool("TRUST_XFF"),
		AIProvider:       getenv("AI_PROVIDER", "deepseek"),
		AIAPIKey:         os.Getenv("AI_API_KEY"),
		AIModel:          os.Getenv("AI_MODEL"),
		AIBaseURL:        os.Getenv("AI_BASE_URL"),
	}
}

func cooldownFromEnv() time.Duration {
	v := os.Getenv("AUTH_FAIL_COOLDOWN")
	if v == "" {
		return 30 * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 30 * time.Second
	}
	return time.Duration(n) * time.Second
}

func envBool(key string) bool {
	v := os.Getenv(key)
	return v == "1" || v == "true" || v == "TRUE"
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
