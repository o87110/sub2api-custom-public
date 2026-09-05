package subscriptioninventory

import (
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

func TestIsExistingUserRenewalEligible(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		status    string
		expiresAt time.Time
		allow     bool
		grace     int
		want      bool
	}{
		{name: "active", status: "active", expiresAt: now.Add(time.Hour), allow: true, want: true},
		{name: "active at expiry", status: "active", expiresAt: now, allow: true, grace: 0, want: false},
		{name: "expired within grace", status: "expired", expiresAt: now.AddDate(0, 0, -3), allow: true, grace: 3, want: true},
		{name: "expired within five days", status: "expired", expiresAt: now.AddDate(0, 0, -5), allow: true, grace: 5, want: true},
		{name: "expired within seven days", status: "expired", expiresAt: now.AddDate(0, 0, -7), allow: true, grace: 7, want: true},
		{name: "expired beyond grace", status: "expired", expiresAt: now.AddDate(0, 0, -4), allow: true, grace: 3, want: false},
		{name: "stale active beyond grace", status: "active", expiresAt: now.AddDate(0, 0, -4), allow: true, grace: 3, want: false},
		{name: "suspended", status: "suspended", expiresAt: now.Add(time.Hour), allow: true, want: false},
		{name: "revoked", status: "revoked", expiresAt: now.Add(time.Hour), allow: true, want: false},
		{name: "disabled policy", status: "active", expiresAt: now.Add(time.Hour), allow: false, want: false},
		{name: "missing", allow: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var snapshot *ExistingSubscriptionSnapshot
			if !tc.expiresAt.IsZero() {
				snapshot = &ExistingSubscriptionSnapshot{Status: tc.status, ExpiresAt: tc.expiresAt}
			}
			if got := IsExistingUserRenewalEligible(tc.allow, tc.grace, snapshot, now); got != tc.want {
				t.Fatalf("eligible = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterPlansForUserShowsSoldOutPlansButRestrictsRenewal(t *testing.T) {
	remaining := 0
	plans := []*dbent.SubscriptionPlan{
		{ID: 1, GroupID: 10, ForSale: true, RemainingQuantity: nil},
		{ID: 2, GroupID: 10, ForSale: false, RemainingQuantity: &remaining, AllowExistingUserRenewal: true},
		{ID: 3, GroupID: 20, ForSale: true, RemainingQuantity: &remaining, AllowExistingUserRenewal: true, RenewalGraceDays: 3},
		{ID: 4, GroupID: 30, ForSale: false, RemainingQuantity: &remaining, AllowExistingUserRenewal: false},
		{ID: 5, GroupID: 40, ForSale: false, RemainingQuantity: nil, AllowExistingUserRenewal: true},
	}

	now := time.Now()
	newBuyer := FilterPlansForUser(plans, nil, now)
	if len(newBuyer) != 4 || newBuyer[0].Plan.ID != 1 || newBuyer[1].Plan.ID != 2 || newBuyer[2].Plan.ID != 3 || newBuyer[3].Plan.ID != 4 {
		t.Fatalf("new buyer plans = %#v, want public and all sold-out plans", newBuyer)
	}
	if newBuyer[1].RenewalAvailable || newBuyer[2].RenewalAvailable {
		t.Fatalf("new buyer sold-out plans unexpectedly allow renewal: %#v", newBuyer)
	}

	existing := FilterPlansForUser(plans, map[int64][]ExistingSubscriptionSnapshot{
		10: {{Status: "active", ExpiresAt: now.Add(time.Hour)}},
		20: {{Status: "expired", ExpiresAt: now.AddDate(0, 0, -1)}},
		30: {{Status: "active", ExpiresAt: now.Add(time.Hour)}},
		40: {{Status: "active", ExpiresAt: now.Add(time.Hour)}},
	}, now)
	if len(existing) != 5 || !existing[1].RenewalAvailable || !existing[2].RenewalAvailable || existing[3].RenewalAvailable || !existing[4].RenewalAvailable {
		t.Fatalf("existing buyer plans = %#v, want all sold-out plans and eligible delisted plan", existing)
	}
}
