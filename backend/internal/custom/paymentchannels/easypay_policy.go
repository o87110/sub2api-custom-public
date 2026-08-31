package paymentchannels

import "strings"

// EasyPayCustomTypeConflict describes why an EasyPay custom payment type is
// not available for configuration.
type EasyPayCustomTypeConflict string

const (
	EasyPayCustomTypeAllowed        EasyPayCustomTypeConflict = ""
	EasyPayCustomTypeExactReserved  EasyPayCustomTypeConflict = "exact_reserved"
	EasyPayCustomTypePrefixReserved EasyPayCustomTypeConflict = "prefix_reserved"
)

var easyPayExactReservedTypes = map[string]struct{}{
	MethodAlipay: {},
	MethodWxpay:  {},
	MethodStripe: {},
	"card":       {},
	"link":       {},
}

// ResolveEasyPayCID selects the CID for an already mapped EasyPay upstream
// payment type. Only Alipay/WeChat upstream types may use their specialized
// CIDs; every other type falls back to the generic CID.
func ResolveEasyPayCID(upstreamType, genericCID, alipayCID, wxpayCID string) string {
	upstreamType = strings.ToLower(strings.TrimSpace(upstreamType))
	genericCID = strings.TrimSpace(genericCID)
	alipayCID = strings.TrimSpace(alipayCID)
	wxpayCID = strings.TrimSpace(wxpayCID)

	switch {
	case strings.HasPrefix(upstreamType, MethodAlipay):
		if alipayCID != "" {
			return alipayCID
		}
	case strings.HasPrefix(upstreamType, MethodWxpay):
		if wxpayCID != "" {
			return wxpayCID
		}
	}
	return genericCID
}

// ClassifyEasyPayCustomType returns the reserved-name classification for a
// canonical EasyPay custom payment type. Syntax validation remains owned by
// the caller; this helper only owns the cross-provider conflict policy.
func ClassifyEasyPayCustomType(methodType string) EasyPayCustomTypeConflict {
	methodType = strings.ToLower(strings.TrimSpace(methodType))
	if _, reserved := easyPayExactReservedTypes[methodType]; reserved {
		return EasyPayCustomTypeExactReserved
	}
	if strings.HasPrefix(methodType, MethodAlipay) || strings.HasPrefix(methodType, MethodWxpay) {
		return EasyPayCustomTypePrefixReserved
	}
	return EasyPayCustomTypeAllowed
}

// AggregateDisplayName returns a display name only when all non-empty names
// agree. Empty values are intentionally ignored so one configured name can be
// used when other provider instances leave the name unset.
func AggregateDisplayName(names []string) string {
	displayName := ""
	for _, name := range names {
		next := strings.TrimSpace(name)
		if next == "" {
			continue
		}
		if displayName == "" {
			displayName = next
			continue
		}
		if displayName != next {
			return ""
		}
	}
	return displayName
}
