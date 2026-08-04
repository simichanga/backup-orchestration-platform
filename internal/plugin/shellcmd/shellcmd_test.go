package shellcmd

import "testing"

func TestQuoteEscapesEmbeddedQuotes(t *testing.T) {
	got := Quote(`o'brien`)
	want := `'o'\''brien'`
	if got != want {
		t.Errorf("Quote(%q) = %q, want %q", `o'brien`, got, want)
	}
}

func TestBuildJoinsQuotedTokens(t *testing.T) {
	got := Build([]string{"tar", "czf", "-", "/some path"})
	want := `'tar' 'czf' '-' '/some path'`
	if got != want {
		t.Errorf("Build() = %q, want %q", got, want)
	}
}
