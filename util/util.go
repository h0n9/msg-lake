package util

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strconv"
)

func CheckStrLen(target string, min, max int) bool {
	l := len(target)
	return l > min && l < max
}

func GenerateRandomBase64String(size int) string {
	bytes := make([]byte, size)
	_, err := rand.Read(bytes)
	if err != nil {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(bytes)
}

func getEnv(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	return value
}

func GetEnv(key, fallback string) string {
	return getEnv(key, fallback)
}

func getEnvInt(key string, fallback int) (int, error) {
	tmpStr := strconv.Itoa(fallback)
	tmpStr = getEnv(key, tmpStr)
	return strconv.Atoi(tmpStr)
}

func GetEnvInt(key string, fallback int) (int, error) {
	return getEnvInt(key, fallback)
}

func GetLogLevel() string {
	return GetEnv("LOG_LEVEL", "info")
}
