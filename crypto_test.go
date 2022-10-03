package wc

import (
	"testing"
)

func TestEncrypt(t *testing.T) {
	data := []byte("test data")

	key := [32]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	iv := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	r, err := encrypt(data, key[:], iv[:])
	if err != nil {
		t.Fatal(err)
	}

	const expected = "39c7737429628697218b008f2c06e81f"
	if r.Data != expected {
		t.Fatalf("unexpected cipher text, got: %s, expected: %s", r.Data, expected)
	}
}
