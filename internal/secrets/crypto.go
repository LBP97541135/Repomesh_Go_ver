package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
)

const formatVersion = 1

type envelope struct {
	id                     VersionID
	owner                  Owner
	purpose                Purpose
	format                 int
	ciphertext, wrappedDEK []byte
	rootID                 string
	revision               int64
}

func (e envelope) aad(wrap bool) []byte {
	parts := []string{"repomesh-secrets", "body", string(e.id), e.owner.Kind, e.owner.ID, string(e.purpose)}
	if wrap {
		parts[1] = "dek"
		parts = append(parts, e.rootID)
	}
	b := binary.BigEndian.AppendUint32(nil, uint32(e.format))
	for _, part := range parts {
		b = binary.BigEndian.AppendUint32(b, uint32(len(part)))
		b = append(b, part...)
	}
	return b
}

func gcm(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	return aead, nil
}

func encrypt(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nil, plaintext, aad), nil
}

func decrypt(key, ciphertext, aad []byte) ([]byte, error) {
	aead, err := gcm(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nil, ciphertext, aad)
	if err != nil {
		return nil, ErrUnavailable
	}
	return plaintext, nil
}

func newVersionID() VersionID {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return VersionID(hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:]))
}

func (e *envelope) seal(root, plaintext []byte) error {
	dek := make([]byte, 32)
	rand.Read(dek)
	defer clear(dek)
	var err error
	e.ciphertext, err = encrypt(dek, plaintext, e.aad(false))
	if err != nil {
		return err
	}
	e.wrappedDEK, err = encrypt(root, dek, e.aad(true))
	return err
}

func (e envelope) open(root []byte) ([]byte, error) {
	if e.format != formatVersion {
		return nil, ErrUnavailable
	}
	dek, err := decrypt(root, e.wrappedDEK, e.aad(true))
	if err != nil {
		return nil, err
	}
	defer clear(dek)
	return decrypt(dek, e.ciphertext, e.aad(false))
}

func (e *envelope) rewrap(oldRoot, newRoot []byte, newRootID string) error {
	if e.format != formatVersion {
		return ErrUnavailable
	}
	dek, err := decrypt(oldRoot, e.wrappedDEK, e.aad(true))
	if err != nil {
		return err
	}
	defer clear(dek)
	plaintext, err := decrypt(dek, e.ciphertext, e.aad(false))
	if err != nil {
		return err
	}
	clear(plaintext)
	e.rootID = newRootID
	e.wrappedDEK, err = encrypt(newRoot, dek, e.aad(true))
	return err
}
