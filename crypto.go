package wc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/pkg/errors"
)

type encrypted struct {
	Data string `json:"data"`
	Hmac string `json:"hmac"`
	Iv   string `json:"iv"`
}

func makeRand(n int) ([]byte, error) {
	b := make([]byte, n)

	_, err := rand.Read(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fill rand bytes")
	}

	return b, nil

}

func MakeKey() ([]byte, error) {
	return makeRand(32)
}

func MakeIV() ([]byte, error) {
	return makeRand(16)
}

func encrypt(data []byte, key []byte, iv []byte) (encrypted, error) {
	var r encrypted

	block, err := aes.NewCipher(key)
	if err != nil {
		return r, errors.Wrap(err, "failed to create cipher")
	}

	padding := block.BlockSize() - len(data)%block.BlockSize()
	ct := make([]byte, len(data)+padding)
	copy(ct, data)

	for i := len(data); i < len(data)+padding; i++ {
		ct[i] = byte(padding)
	}

	enc := cipher.NewCBCEncrypter(block, iv)
	enc.CryptBlocks(ct, ct)

	hm := hmac.New(sha256.New, key)

	hmd := make([]byte, len(ct)+len(iv))

	copy(hmd[0:], ct)
	copy(hmd[len(ct):], iv)

	_, err = hm.Write(hmd)
	if err != nil {
		return r, errors.Wrap(err, "failed to write")
	}

	hmacsha256 := hm.Sum(nil)

	r = encrypted{
		Data: hex.EncodeToString(ct),
		Hmac: hex.EncodeToString(hmacsha256),
		Iv:   hex.EncodeToString(iv),
	}

	return r, nil
}
