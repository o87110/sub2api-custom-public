package paymentchannels

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

const maxChannelDisplayNameRunes = 100

var channelIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ChannelSetting customizes one user-facing, provider-grouped payment channel.
// A nil FeeRate inherits the global default while an explicit zero disables fees.
type ChannelSetting struct {
	DisplayName string   `json:"display_name,omitempty"`
	FeeRate     *float64 `json:"fee_rate,omitempty"`
}

// UnmarshalJSON keeps admin API requests as strict as persisted configuration.
func (s *ChannelSetting) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		return fmt.Errorf("payment channel setting must be a JSON object")
	}
	type channelSettingJSON ChannelSetting
	var decoded channelSettingJSON
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	*s = ChannelSetting(decoded)
	return nil
}

// ChannelSettings is keyed by the stable channel ID returned by StableID.
type ChannelSettings map[string]ChannelSetting

// ParseChannelSettings parses the JSON value stored in the existing settings table.
// Invalid persisted configuration fails closed instead of silently changing prices.
func ParseChannelSettings(raw string) (ChannelSettings, error) {
	if strings.TrimSpace(raw) == "" {
		return ChannelSettings{}, nil
	}

	var input ChannelSettings
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("decode payment channel settings: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, fmt.Errorf("decode payment channel settings: expected a JSON object")
	}
	return NormalizeChannelSettings(input)
}

// NormalizeChannelSettings validates and canonicalizes settings before storage.
func NormalizeChannelSettings(input ChannelSettings) (ChannelSettings, error) {
	normalized := make(ChannelSettings)
	for channelID, setting := range input {
		if !channelIDPattern.MatchString(channelID) {
			return nil, fmt.Errorf("invalid payment channel id %q", channelID)
		}

		for _, r := range setting.DisplayName {
			if unicode.IsControl(r) {
				return nil, fmt.Errorf("payment channel %q display name contains control characters", channelID)
			}
		}
		displayName := strings.TrimSpace(setting.DisplayName)
		if utf8.RuneCountInString(displayName) > maxChannelDisplayNameRunes {
			return nil, fmt.Errorf("payment channel %q display name exceeds %d characters", channelID, maxChannelDisplayNameRunes)
		}

		var feeRate *float64
		if setting.FeeRate != nil {
			value := *setting.FeeRate
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
				return nil, fmt.Errorf("payment channel %q fee rate must be between 0 and 100", channelID)
			}
			decimalValue := decimal.NewFromFloat(value)
			if !decimalValue.Equal(decimalValue.Round(2)) {
				return nil, fmt.Errorf("payment channel %q fee rate allows at most 2 decimal places", channelID)
			}
			feeRate = &value
		}

		if displayName == "" && feeRate == nil {
			continue
		}
		normalized[channelID] = ChannelSetting{
			DisplayName: displayName,
			FeeRate:     feeRate,
		}
	}
	return normalized, nil
}

// SerializeChannelSettings validates settings and returns their canonical JSON.
func SerializeChannelSettings(input ChannelSettings) (string, ChannelSettings, error) {
	normalized, err := NormalizeChannelSettings(input)
	if err != nil {
		return "", nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", nil, fmt.Errorf("encode payment channel settings: %w", err)
	}
	return string(encoded), normalized, nil
}

// ApplyChannelSettings applies display-name and fee overrides without mutating
// the caller's option slice.
func ApplyChannelSettings(options []MethodOption, settings ChannelSettings) []MethodOption {
	result := append([]MethodOption(nil), options...)
	for i := range result {
		setting, ok := settings[result[i].ID]
		if !ok {
			continue
		}
		if setting.DisplayName != "" {
			result[i].DisplayName = setting.DisplayName
		}
		if setting.FeeRate != nil {
			result[i].FeeRate = *setting.FeeRate
		}
	}
	return result
}

// ResolveFeeRate returns the selected channel override. Provider-agnostic
// requests intentionally keep the global default for legacy compatibility.
func ResolveFeeRate(paymentType, providerKey string, globalFeeRate float64, settings ChannelSettings) float64 {
	if normalize(providerKey) == "" {
		return globalFeeRate
	}
	setting, ok := settings[StableID(paymentType, providerKey)]
	if !ok || setting.FeeRate == nil {
		return globalFeeRate
	}
	return *setting.FeeRate
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode payment channel settings: multiple JSON values")
		}
		return fmt.Errorf("decode payment channel settings: %w", err)
	}
	return nil
}
