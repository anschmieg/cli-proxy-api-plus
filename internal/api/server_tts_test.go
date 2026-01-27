package api

import "testing"

func TestSanitizeBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "http with trailing slash",
			raw:  "http://edge-tts:5050/",
			want: "http://edge-tts:5050",
		},
		{
			name: "https with path",
			raw:  "https://example.com/base/",
			want: "https://example.com/base",
		},
		{
			name: "empty",
			raw:  "",
			want: "",
		},
		{
			name: "missing scheme",
			raw:  "edge-tts:5050",
			want: "",
		},
		{
			name: "unsupported scheme",
			raw:  "ftp://example.com",
			want: "",
		},
		{
			name: "missing host",
			raw:  "http://",
			want: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeBaseURL(tc.raw); got != tc.want {
				t.Fatalf("sanitizeBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestResolveTTSBaseURL(t *testing.T) {
	t.Run("edge env wins", func(t *testing.T) {
		t.Setenv("EDGE_TTS_BASE_URL", "http://edge-tts:5050/")
		t.Setenv("TTS_BASE_URL", "http://other:5050")
		if got := resolveTTSBaseURL(); got != "http://edge-tts:5050" {
			t.Fatalf("resolveTTSBaseURL() = %q, want %q", got, "http://edge-tts:5050")
		}
	})

	t.Run("tts env used when edge empty", func(t *testing.T) {
		t.Setenv("EDGE_TTS_BASE_URL", "")
		t.Setenv("TTS_BASE_URL", "http://tts:5050/")
		if got := resolveTTSBaseURL(); got != "http://tts:5050" {
			t.Fatalf("resolveTTSBaseURL() = %q, want %q", got, "http://tts:5050")
		}
	})

	t.Run("default used", func(t *testing.T) {
		t.Setenv("EDGE_TTS_BASE_URL", "")
		t.Setenv("TTS_BASE_URL", "")
		if got := resolveTTSBaseURL(); got != "http://edge-tts:5050" {
			t.Fatalf("resolveTTSBaseURL() = %q, want %q", got, "http://edge-tts:5050")
		}
	})
}
