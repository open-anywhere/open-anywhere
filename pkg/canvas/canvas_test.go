package canvas

import "testing"

func TestDescription(t *testing.T) {
	if got, want := Description(), "1280×720（16:9）"; got != want {
		t.Fatalf("Description() = %q, want %q", got, want)
	}
}

func TestIs16x9(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want bool
	}{
		{1280, 720, true},
		{1920, 1080, true},
		{3840, 2160, true},
		{640, 360, true},
		{640, 480, false},
		{1280, 721, false},
		{0, 720, false},
		{-1280, -720, false},
	} {
		if got := Is16x9(tc.w, tc.h); got != tc.want {
			t.Errorf("Is16x9(%d, %d) = %v, want %v", tc.w, tc.h, got, tc.want)
		}
	}
}
