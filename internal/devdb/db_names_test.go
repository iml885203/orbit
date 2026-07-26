package devdb

import "testing"

// safeArgName is the looser of the two db-workflow input grammars (safeDBName
// is covered in handlers_dbops_test.go). It's this session's new injection
// boundary for the positional db/project argument, so lock its accept/reject
// sets: it must admit project names yet still reject metacharacters.

func TestSafeArgName(t *testing.T) {
	// Looser than safeDBName because it must admit project names (folder
	// names carry '.' and '-'), but it still rejects injection metacharacters.
	valid := []string{"WalletDB", "dbproject.info", "foo-bar", "a.b.c", "_x", "1digit-ok"}
	for _, s := range valid {
		if !safeArgName.MatchString(s) {
			t.Errorf("safeArgName should accept a db-or-project name %q", s)
		}
	}
	invalid := []string{
		"",         // empty
		"bad;name", // statement separator
		"a b",      // whitespace
		"a'b",      // quote
		"a\"b",     // double quote
		"a`b",      // backtick
		"a$(x)",    // command substitution
		"a|b",      // pipe
		"a/b",      // path separator
		"a\\b",     // backslash
		"a\nb",     // newline
	}
	for _, s := range invalid {
		if safeArgName.MatchString(s) {
			t.Errorf("safeArgName must reject injection payload %q", s)
		}
	}
}
