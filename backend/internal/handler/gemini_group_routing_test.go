package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGeminiCandidateModelPreservesCompositeFallbackModel(t *testing.T) {
	require.Equal(t, "group-b-model", geminiCandidateModelAfterChannelMapping(
		"group-b-model",
		service.ChannelMappingResult{MappedModel: "group-a-model"},
	))
	require.Equal(t, "channel-b-model", geminiCandidateModelAfterChannelMapping(
		"group-b-model",
		service.ChannelMappingResult{Mapped: true, MappedModel: "channel-b-model"},
	))
}

func TestGeminiCandidateModelUsesEachGroupsOwnChannelMapping(t *testing.T) {
	require.Equal(t, "channel-a-model", geminiCandidateModelAfterChannelMapping(
		"group-a-model",
		service.ChannelMappingResult{Mapped: true, MappedModel: "channel-a-model"},
	))
	require.Equal(t, "channel-b-model", geminiCandidateModelAfterChannelMapping(
		"group-b-model",
		service.ChannelMappingResult{Mapped: true, MappedModel: "channel-b-model"},
	))
}
