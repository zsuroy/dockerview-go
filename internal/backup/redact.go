package backup

import (
	"sort"
	"strings"
)

// MaskedValue replaces secret env values in summaries/<...>.json.
const MaskedValue = "***MASKED***"

// secretKeyPatterns marks an env KEY as sensitive when any pattern appears in
// the upper-cased key. Over-masking is preferred over leaking: the catch-all
// "KEY" also covers API_KEY/SSH_KEY/etc. (see BACKUP_SECURITY §3).
var secretKeyPatterns = []string{
	"PASSWORD",
	"PASSWD",
	"SECRET",
	"TOKEN",
	"APIKEY",
	"API_KEY",
	"ACCESSKEY",
	"ACCESS_KEY",
	"PRIVATE",
	"CREDENTIAL",
	"AUTH",
	"CONNECTION",
	"KEY", // catch-all: SSH_KEY, ENCRYPTION_KEY, …
}

// IsSecretKey reports whether an environment variable key looks sensitive.
func IsSecretKey(key string) bool {
	up := strings.ToUpper(key)
	for _, p := range secretKeyPatterns {
		if strings.Contains(up, p) {
			return true
		}
	}
	return false
}

// RedactEnv masks sensitive entries of KEY=VALUE env lists. It returns the
// masked list (same order) plus the sorted names of redacted keys. Entries
// without '=' are kept verbatim; an empty input yields empty outputs.
func RedactEnv(env []string) ([]string, []string) {
	if len(env) == 0 {
		return []string{}, []string{}
	}
	out := make([]string, 0, len(env))
	redacted := make([]string, 0)
	for _, e := range env {
		key, _, hasValue := strings.Cut(e, "=")
		if hasValue && IsSecretKey(key) {
			out = append(out, key+"="+MaskedValue)
			redacted = append(redacted, key)
			continue
		}
		out = append(out, e)
	}
	sort.Strings(redacted)
	return out, redacted
}
