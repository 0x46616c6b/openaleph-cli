package output

import "testing"

func TestDuration(t *testing.T) {
	cases := map[string]string{
		"PT5H12M42.885S":  "5h12m",
		"P2DT9H28M10.95S": "2d9h",
		"PT0S":            "-",
		"PT45S":           "0m45s",
		"PT8M":            "8m",
		"":                "",
		"soon":            "soon",
	}
	for in, want := range cases {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	cases := map[float64]string{0: "░░░░   0%", 0.5: "██░░  50%", 1: "████ 100%", -1: "░░░░   0%", 2: "████ 100%"}
	for in, want := range cases {
		if got := ProgressBar(in, 4); got != want {
			t.Errorf("ProgressBar(%v) = %q, want %q", in, got, want)
		}
	}
}
