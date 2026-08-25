package volume

import "testing"

func TestSplitShortPreservesDestinationAndMode(t *testing.T) {
	tests := []struct {
		value  string
		source string
		suffix string
	}{
		{"data:/var/lib/data", "data", ":/var/lib/data"},
		{"./fixtures/init.sql:/docker-entrypoint-initdb.d/init.sql:ro", "./fixtures/init.sql", ":/docker-entrypoint-initdb.d/init.sql:ro"},
		{`C:\work\init.sql:/docker-entrypoint-initdb.d/init.sql:ro`, `C:\work\init.sql`, ":/docker-entrypoint-initdb.d/init.sql:ro"},
		{"C:/work/init.sql:/docker-entrypoint-initdb.d/init.sql:ro", "C:/work/init.sql", ":/docker-entrypoint-initdb.d/init.sql:ro"},
	}
	for _, test := range tests {
		source, suffix := SplitShort(test.value)
		if source != test.source || suffix != test.suffix {
			t.Errorf("SplitShort(%q) = (%q, %q), want (%q, %q)", test.value, source, suffix, test.source, test.suffix)
		}
	}
}

func TestIsBindSource(t *testing.T) {
	for _, source := range []string{"./fixture", "../fixture", "/tmp/fixture", "~/fixture", `C:\fixture`, "C:/fixture"} {
		if !IsBindSource(source) {
			t.Errorf("IsBindSource(%q) = false", source)
		}
	}
	for _, source := range []string{"data", "cache.name", "C"} {
		if IsBindSource(source) {
			t.Errorf("IsBindSource(%q) = true", source)
		}
	}
}
