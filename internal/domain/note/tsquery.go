package note

import "strings"

// toPrefixTSQuery turns a raw search term into a prefix-matching to_tsquery
// string, e.g. "roa" -> "roa:*", so typing a partial word still matches while
// it's being typed. Mirrors the search domain's helper of the same name —
// duplicated rather than imported cross-domain because it is unexported there
// and this package must not depend on search's internals for one helper.
// Returns "" when the term has no alphanumeric content; callers treat that as
// a guaranteed-empty match rather than passing an invalid query to Postgres.
func toPrefixTSQuery(term string) string {
	fields := strings.FieldsFunc(strings.ToLower(term), func(r rune) bool {
		return !isAlnum(r)
	})
	lexemes := make([]string, 0, len(fields))
	for _, f := range fields {
		lexemes = append(lexemes, f+":*")
	}
	return strings.Join(lexemes, " & ")
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		r > 127
}
