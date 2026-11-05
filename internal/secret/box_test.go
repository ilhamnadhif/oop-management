package secret

import (
	"errors"
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef"

func TestBoxReadsBackWhatItSealed(t *testing.T) {
	box, err := NewBox(testKey)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	sealed, err := box.Seal("rahasia kendal", "PRJ-0002")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(sealed, "rahasia") {
		t.Fatalf("the value can be read off the cell: %s", sealed)
	}
	if plain, err := box.Open(sealed, "PRJ-0002"); err != nil || plain != "rahasia kendal" {
		t.Fatalf("open = %q, %v", plain, err)
	}

	// The same password twice must not read the same, or a sheet would show
	// which projects share one.
	again, err := box.Seal("rahasia kendal", "PRJ-0002")
	if err != nil {
		t.Fatalf("seal again: %v", err)
	}
	if again == sealed {
		t.Fatal("two seals of one value are identical")
	}
}

func TestBoxRefusesWhatIsNotItsOwn(t *testing.T) {
	box, err := NewBox(testKey)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	other, err := NewBox(strings.Repeat("z", MinKeyLength))
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	sealed, err := box.Seal("rahasia", "PRJ-0002")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	for name, open := range map[string]func() (string, error){
		"another project": func() (string, error) { return box.Open(sealed, "PRJ-0001") },
		"another key":     func() (string, error) { return other.Open(sealed, "PRJ-0002") },
		"an edited cell":  func() (string, error) { return box.Open(sealed[:len(sealed)-2]+"xx", "PRJ-0002") },
		"plain text":      func() (string, error) { return box.Open("rahasia", "PRJ-0002") },
		"a truncated one": func() (string, error) { return box.Open("v1.abc", "PRJ-0002") },
	} {
		if plain, err := open(); !errors.Is(err, ErrCannotOpen) || plain != "" {
			t.Fatalf("%s opened: %q, %v", name, plain, err)
		}
	}
}

func TestBoxRefusesAShortKey(t *testing.T) {
	if _, err := NewBox("pendek"); err == nil {
		t.Fatal("a short key was accepted")
	}
}
