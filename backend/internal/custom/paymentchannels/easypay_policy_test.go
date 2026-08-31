package paymentchannels

import "testing"

func TestResolveEasyPayCID(t *testing.T) {
	tests := []struct {
		name                            string
		upstreamType                    string
		genericCID, alipayCID, wxpayCID string
		want                            string
	}{
		{name: "unknown type uses generic even with wxpay cid", upstreamType: "usdt", genericCID: "cid-generic", wxpayCID: "cid-wxpay", want: "cid-generic"},
		{name: "unknown type uses generic when specialized is also set", upstreamType: "epay", genericCID: "cid-generic", alipayCID: "cid-alipay", wxpayCID: "cid-wxpay", want: "cid-generic"},
		{name: "alipay prefers specialized cid", upstreamType: "alipay", genericCID: "cid-generic", alipayCID: "cid-alipay", wxpayCID: "cid-wxpay", want: "cid-alipay"},
		{name: "alipay falls back to generic", upstreamType: "alipay_h5", genericCID: "cid-generic", wxpayCID: "cid-wxpay", want: "cid-generic"},
		{name: "wxpay prefers specialized cid", upstreamType: "wxpay", genericCID: "cid-generic", alipayCID: "cid-alipay", wxpayCID: "cid-wxpay", want: "cid-wxpay"},
		{name: "wxpay falls back to generic", upstreamType: "wxpay_h5", genericCID: "cid-generic", alipayCID: "cid-alipay", want: "cid-generic"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveEasyPayCID(test.upstreamType, test.genericCID, test.alipayCID, test.wxpayCID); got != test.want {
				t.Fatalf("ResolveEasyPayCID(%q, %q, %q, %q) = %q, want %q", test.upstreamType, test.genericCID, test.alipayCID, test.wxpayCID, got, test.want)
			}
		})
	}
}

func TestClassifyEasyPayCustomType(t *testing.T) {
	tests := []struct {
		methodType string
		want       EasyPayCustomTypeConflict
	}{
		{methodType: "alipay", want: EasyPayCustomTypeExactReserved},
		{methodType: "wxpay", want: EasyPayCustomTypeExactReserved},
		{methodType: "stripe", want: EasyPayCustomTypeExactReserved},
		{methodType: "card", want: EasyPayCustomTypeExactReserved},
		{methodType: "link", want: EasyPayCustomTypeExactReserved},
		{methodType: "alipay_hk", want: EasyPayCustomTypePrefixReserved},
		{methodType: "wxpay_custom", want: EasyPayCustomTypePrefixReserved},
		{methodType: "easypay", want: EasyPayCustomTypeAllowed},
		{methodType: "airwallex", want: EasyPayCustomTypeAllowed},
		{methodType: "usdt_trc20", want: EasyPayCustomTypeAllowed},
		{methodType: "ldc", want: EasyPayCustomTypeAllowed},
	}
	for _, test := range tests {
		t.Run(test.methodType, func(t *testing.T) {
			if got := ClassifyEasyPayCustomType(test.methodType); got != test.want {
				t.Fatalf("ClassifyEasyPayCustomType(%q) = %q, want %q", test.methodType, got, test.want)
			}
		})
	}
}

func TestAggregateDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{name: "all empty", names: []string{"", "  "}, want: ""},
		{name: "one non-empty", names: []string{"", " USDT ", ""}, want: "USDT"},
		{name: "all equal", names: []string{"USDT", " USDT ", ""}, want: "USDT"},
		{name: "different non-empty", names: []string{"USDT", "Tether"}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AggregateDisplayName(test.names); got != test.want {
				t.Fatalf("AggregateDisplayName(%q) = %q, want %q", test.names, got, test.want)
			}
		})
	}
}
