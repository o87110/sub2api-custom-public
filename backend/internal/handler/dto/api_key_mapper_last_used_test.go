package dto

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromService_MapsLastUsedAt(t *testing.T) {
	lastUsed := time.Now().UTC().Truncate(time.Second)
	lastUsedIP := "203.0.113.10"
	src := &service.APIKey{
		ID:                 1,
		UserID:             2,
		Key:                "sk-map-last-used",
		Name:               "Mapper",
		Status:             service.StatusActive,
		LastUsedAt:         &lastUsed,
		LastUsedIP:         &lastUsedIP,
		CurrentConcurrency: 3,
	}

	out := APIKeyFromService(src)
	require.NotNil(t, out)
	require.NotNil(t, out.LastUsedAt)
	require.WithinDuration(t, lastUsed, *out.LastUsedAt, time.Second)
	require.NotNil(t, out.LastUsedIP)
	require.Equal(t, lastUsedIP, *out.LastUsedIP)
	require.Equal(t, 3, out.CurrentConcurrency)
}

func TestAPIKeyFromService_MapsNilLastUsedAt(t *testing.T) {
	src := &service.APIKey{
		ID:     1,
		UserID: 2,
		Key:    "sk-map-last-used-nil",
		Name:   "MapperNil",
		Status: service.StatusActive,
	}

	out := APIKeyFromService(src)
	require.NotNil(t, out)
	require.Nil(t, out.LastUsedAt)
	require.Nil(t, out.LastUsedIP)
	require.NotNil(t, out.GroupIDs)
	require.NotNil(t, out.Groups)
	require.Nil(t, out.Group)
}

func TestInt64SliceFieldTracksPresenceAndNormalizesNull(t *testing.T) {
	var omitted Int64SliceField
	require.False(t, omitted.Set)
	require.Nil(t, omitted.Pointer())

	var nullValue Int64SliceField
	require.NoError(t, nullValue.UnmarshalJSON([]byte("null")))
	require.True(t, nullValue.Set)
	require.NotNil(t, nullValue.Pointer())
	require.Empty(t, *nullValue.Pointer())

	var values Int64SliceField
	require.NoError(t, values.UnmarshalJSON([]byte(`[3,1]`)))
	require.True(t, values.Set)
	require.Equal(t, []int64{3, 1}, values.Value)
}
