package moderation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildKeywordExcerptFromRedacted(t *testing.T) {
	t.Run("short input remains complete", func(t *testing.T) {
		const text = "普通中文输入"
		require.Equal(t, text, BuildKeywordExcerptFromRedacted(text, "风控绕过", 1000))
	})

	t.Run("keyword in leading excerpt keeps leading text", func(t *testing.T) {
		text := "风控绕过" + strings.Repeat("后", 1000)
		excerpt := BuildKeywordExcerptFromRedacted(text, "风控绕过", 1000)

		require.Len(t, []rune(excerpt), 1000)
		require.Equal(t, string([]rune(text)[:1000]), excerpt)
	})

	t.Run("late Chinese keyword keeps head and matching context", func(t *testing.T) {
		text := strings.Repeat("开", 1200) + "这里出现风控绕过关键词" + strings.Repeat("后", 800)
		excerpt := BuildKeywordExcerptFromRedacted(text, "风控绕过", 1000)

		require.Len(t, []rune(excerpt), 1000)
		require.True(t, strings.HasPrefix(excerpt, strings.Repeat("开", 350)))
		require.Contains(t, excerpt, KeywordContextSeparator)
		require.Contains(t, excerpt, "风控绕过")
		require.Contains(t, excerpt, "后")
	})

	t.Run("late keyword lookup is case insensitive", func(t *testing.T) {
		text := strings.Repeat("开", 1200) + "检测到 BYPASS 风险"
		require.Contains(t, BuildKeywordExcerptFromRedacted(text, "bypass", 1000), "BYPASS")
	})

	t.Run("matcher lowercase semantics locate the ASCII match", func(t *testing.T) {
		text := strings.Repeat("前", 1200) + "ſ" + strings.Repeat("中", 800) + "s" + strings.Repeat("后", 100)
		excerpt := BuildKeywordExcerptFromRedacted(text, "s", 1000)

		require.Contains(t, excerpt, KeywordContextSeparator)
		require.Contains(t, excerpt, "s")
		require.NotContains(t, excerpt, "ſ")
	})
}
