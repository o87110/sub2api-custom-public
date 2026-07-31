package paymentchannels

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

type RevisionRecord struct {
	ProviderKey    string
	Config         string
	SupportedTypes string
	PaymentMode    string
	Limits         string
	Enabled        bool
}

type SelectionSnapshot struct {
	ProviderKey string
	Revision    string
}

func InstanceRevision(record RevisionRecord) string {
	payload := strings.Join([]string{
		strings.TrimSpace(record.ProviderKey),
		record.Config,
		record.SupportedTypes,
		record.PaymentMode,
		record.Limits,
		strconv.FormatBool(record.Enabled),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", sum)
}

func SelectionMatches(record RevisionRecord, selection SelectionSnapshot, supportsMethod bool) bool {
	return record.Enabled &&
		supportsMethod &&
		normalize(record.ProviderKey) == normalize(selection.ProviderKey) &&
		strings.TrimSpace(selection.Revision) != "" &&
		InstanceRevision(record) == selection.Revision
}
