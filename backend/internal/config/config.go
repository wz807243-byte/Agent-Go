package config

import (
	"os"
	"strings"
)

func Agent() (endpoint, model, key string) {
	endpoint, model = os.Getenv("REVIEW_URL"), os.Getenv("REVIEW_MODEL")
	if endpoint == "" {
		endpoint = "https://api.deepseek.com/chat/completions"
	}
	if model == "" {
		model = "deepseek-flash"
	}
	key = strings.TrimSpace(os.Getenv("REVIEW_API_KEY"))
	return
}

func Review() (endpoint, model, key string) {
	endpoint, model = os.Getenv("REVIEW_URL"), os.Getenv("REVIEW_MODEL")
	if endpoint == "" {
		endpoint = "https://api.deepseek.com/chat/completions"
	}
	if model == "" && endpoint == "https://api.deepseek.com/chat/completions" {
		model = "deepseek-flash"
	}
	key = os.Getenv("REVIEW_API_KEY")
	return
}
