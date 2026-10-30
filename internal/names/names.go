// Package names holds the rules for machine names on a Wardline network: what a short name looks like, how a
// taken one is numbered, and how a full name relates to it. See specs/BUSINESS-RULES.md, "Names for machines".
package names

import (
	"errors"
	"strconv"
	"strings"
)

// Domain is the fixed ending of every name on the network. ".internal" is set aside for private networks.
const Domain = "wardline.internal"

// ServerName is what the server is always called.
const ServerName = "server"

const maxLabel = 63

var reserved = map[string]bool{ServerName: true, "localhost": true, "wardline": true}

// Reserved says whether a machine may not use the name.
func Reserved(label string) bool { return reserved[label] }

// Slug turns a machine's name into a short name: lowercase letters and digits, one hyphen between runs of
// anything else, no hyphen at either end, at most 63 characters. Nothing usable left means "machine".
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	s := b.String()
	if len(s) > maxLabel {
		s = strings.TrimRight(s[:maxLabel], "-")
	}
	if s == "" {
		return "machine"
	}
	return s
}

// Assign returns want, or want with -2, -3 and so on, until free says the name is available. Reserved names are numbered too.
func Assign(want string, free func(string) bool) string {
	if !Reserved(want) && free(want) {
		return want
	}
	for n := 2; ; n++ {
		suffix := "-" + strconv.Itoa(n)
		base := want
		if len(base)+len(suffix) > maxLabel {
			base = strings.TrimRight(base[:maxLabel-len(suffix)], "-")
		}
		if c := base + suffix; !Reserved(c) && free(c) {
			return c
		}
	}
}

// Check says whether a name typed by hand (a short name or an extra name) is acceptable.
func Check(label string) error {
	if label == "" || len(label) > maxLabel {
		return errors.New("A name can be 1 to 63 characters.")
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return errors.New("A name cannot start or end with a hyphen.")
	}
	for _, r := range label {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return errors.New("Use only lowercase letters, numbers and hyphens.")
		}
	}
	if Reserved(label) {
		return errors.New("That name is kept for Wardline. Choose a different one.")
	}
	return nil
}

// Full is a short name with the network's ending.
func Full(label string) string { return label + "." + Domain }

// Short takes a name a machine asked for and returns the part before the ending, if it is a one-part name under the ending.
func Short(fqdn string) (string, bool) {
	q := strings.TrimSuffix(strings.ToLower(fqdn), ".")
	label, ok := strings.CutSuffix(q, "."+Domain)
	if !ok || label == "" || strings.Contains(label, ".") {
		return "", false
	}
	return label, true
}
