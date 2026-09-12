package ports

import "testing"

func TestParseSet(t *testing.T) {
	set, err := ParseSet("3000, 8000-8002")
	if err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{3000, 8000, 8001, 8002} {
		if !set.Contains(port) {
			t.Errorf("expected set to contain %d", port)
		}
	}
	if set.Contains(8003) {
		t.Error("did not expect set to contain 8003")
	}
}

func TestParseSetRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"0", "65536", "hi", "9-2", "1-2-3", "1,,2"} {
		if _, err := ParseSet(value); err == nil {
			t.Errorf("ParseSet(%q) unexpectedly succeeded", value)
		}
	}
}
