package util

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckStrLen(t *testing.T) {
	assert.True(t, CheckStrLen("abc", 1, 5), "length is between min and max")
	assert.False(t, CheckStrLen("abcdef", 1, 5), "length exceeds max")
	assert.False(t, CheckStrLen("a", 2, 4), "length is below min")
	assert.False(t, CheckStrLen("", 0, 10), "empty string is within range")
	assert.False(t, CheckStrLen("hello", 5, 5), "length equals max (excluded)")
}

func TestGenerateRandomBase64String(t *testing.T) {
	// Test cases
	size := 10
	randomString := GenerateRandomBase64String(10)

	// Check if it's a valid base64 string
	decodedBytes, err := base64.RawStdEncoding.DecodeString(randomString)
	assert.NoError(t, err, "generated string should be a valid base64 string")

	// Check if decoded length matches size
	assert.Equal(t, size, len(decodedBytes), "decoded string length should match size")
}

func TestGetEnv(t *testing.T) {
	assert.Equal(t, "default value", GetEnv("ENV", "default value"))
	os.Setenv("ENV", "explicit value")
	assert.Equal(t, "explicit value", GetEnv("ENV", "default value"))
}

func TestGetLogLevel(t *testing.T) {
	os.Setenv("LOG_LEVEL", "info")
	assert.Equal(t, "info", GetLogLevel())
	os.Setenv("LOG_LEVEL", "debug")
	assert.Equal(t, "debug", GetLogLevel())
}
