package canon

import (
	"fmt"
	"strings"
)

func validateCanonicalBodyCoverage(rawText string, canonicalBody string) error {
	raw := strings.TrimSpace(rawText)
	body := strings.TrimSpace(canonicalBody)
	if raw == "" || body == "" {
		return nil
	}
	rawWords := len(strings.Fields(raw))
	bodyWords := len(strings.Fields(body))
	if rawWords < 120 {
		return nil
	}
	minimum := int(float64(rawWords) * 0.18)
	if minimum < 60 {
		minimum = 60
	}
	if bodyWords < minimum {
		return fmt.Errorf("ai canonicalization compressed source too much: source_words=%d canonical_words=%d minimum=%d", rawWords, bodyWords, minimum)
	}
	return nil
}
