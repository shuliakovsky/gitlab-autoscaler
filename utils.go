package main

import "strings"

// ANSI Colors
const (
	Reset     = "\033[0m"
	Red       = "\033[31m"
	Green     = "\033[32m"
	Yellow    = "\033[33m"
	Blue      = "\033[34m"
	Magenta   = "\033[35m"
	Cyan      = "\033[36m"
	LightGray = "\033[37m"
)

func split(s, delimiter string) []string {
	var result []string
	for _, item := range strings.Split(s, delimiter) {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
