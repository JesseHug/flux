package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// aesCrypto implements the panel's SHA-256 + AES-256-GCM envelope without importing x/internal.
type aesCrypto struct {
	key []byte
}

func newAESCrypto(secret string) (*aesCrypto, error) {
	if secret == "" {
		return nil, fmt.Errorf("密钥不能为空")
	}
	hash := sha256.Sum256([]byte(secret))
	return &aesCrypto{key: hash[:]}, nil
}

func (a *aesCrypto) Encrypt(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("待加密数据不能为空")
	}
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", fmt.Errorf("创建 AES cipher 失败: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("创建 GCM 失败: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成 nonce 失败: %v", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (a *aesCrypto) Decrypt(encryptedData string) ([]byte, error) {
	if encryptedData == "" {
		return nil, fmt.Errorf("加密数据不能为空")
	}
	encrypted, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %v", err)
	}
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, fmt.Errorf("创建 AES cipher 失败: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 失败: %v", err)
	}
	nonceSize := gcm.NonceSize()
	if len(encrypted) < nonceSize {
		return nil, fmt.Errorf("加密数据长度不足")
	}
	return gcm.Open(nil, encrypted[:nonceSize], encrypted[nonceSize:], nil)
}
