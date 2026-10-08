package invitationhint

import (
	"fmt"
	"net/url"
	"strings"
)

// Normalize 去除配置空白，并拒绝不能安全打开的网址。
func Normalize(text, link string) (string, string, error) {
	text = strings.TrimSpace(text)
	link = strings.TrimSpace(link)
	if link != "" && !ValidURL(link) {
		return "", "", fmt.Errorf("invitation code hint URL must be an absolute http(s) URL")
	}
	return text, link, nil
}

// ValidURL 仅允许完整的 HTTP(S) 网页地址。
func ValidURL(link string) bool {
	if link == "" || strings.ContainsAny(link, "\\\r\n\t") {
		return false
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Opaque != "" || parsed.User != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Hostname() != ""
}

// SafeURL 在读取公开配置时过滤历史或异常写入的不安全网址。
func SafeURL(link string) string {
	link = strings.TrimSpace(link)
	if !ValidURL(link) {
		return ""
	}
	return link
}
