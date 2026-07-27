package ratedisplay

import (
	"math"
	"strings"
	"unicode/utf8"
)

const (
	Placeholder      = "{rate}"
	MaxTemplateRunes = 64
)

func ValidOverride(value *float64) bool {
	if value == nil {
		return true
	}
	return !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value > 0
}

func NormalizeTemplate(value string) (string, bool) {
	template := strings.TrimSpace(value)
	if template == "" {
		return "", true
	}
	if utf8.RuneCountInString(template) > MaxTemplateRunes ||
		strings.Count(template, Placeholder) != 1 {
		return "", false
	}
	return template, true
}
