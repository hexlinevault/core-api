package cipher_test

import (
	"crypto/aes"
	"fmt"
	"testing"

	"github.com/hexlinevault/core-api/helpers/cipher"

	"gotest.tools/assert"
)

func TestCipher(t *testing.T) {
	key := []byte("abcdefghIJKLMNAA")
	plaintext := "1"
	benc, err := cipher.Encrypt(plaintext, key)
	assert.Equal(t, err, nil)
	denc, err := cipher.Decrypt(benc, key)
	assert.Equal(t, err, nil)
	assert.Equal(t, denc, plaintext)
	fmt.Println("Encode Result:\t", benc)
	fmt.Println("Decode Result:\t", denc)
}

func TestAESCipher(t *testing.T) {
	key := "AKJSDKJ!(@#!#1aksdfjask121312212" // 32 char
	iv := "sdf!@dfAKJK12231"                  // 16 char
	plaintext := "1"
	// fmt.Println("Data to encode: ", plaintext)

	cipherText, err := cipher.AES256(plaintext, key, iv, aes.BlockSize)
	assert.Equal(t, err, nil)
	denc, err := cipher.AES256Decode(cipherText, key, iv)
	assert.Equal(t, err, nil)
	fmt.Println("Encode Result:\t", cipherText)
	fmt.Println("Decode Result:\t", denc)
}
