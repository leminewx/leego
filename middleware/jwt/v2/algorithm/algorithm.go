package algorithm

import (
	"crypto"
	"fmt"
)

// Algorithm 定义算法的类型
type Algorithm string

const (
	HS256 Algorithm = "HS256"
	HS384 Algorithm = "HS384"
	HS512 Algorithm = "HS512"
)

// Signer 签名接口
type Signer interface {
	Sign(data []byte, key []byte) ([]byte, error)
	Verify(data []byte, signature []byte, key []byte) (bool, error)
	Name() string
	Hash() crypto.Hash
}

// Registry 算法注册表
type Registry struct {
	signers map[Algorithm]Signer
}

// NewRegistry 创建算法注册表
func NewRegistry() *Registry {
	return &Registry{
		signers: make(map[Algorithm]Signer),
	}
}

// Register 注册算法
func (r *Registry) Register(alg Algorithm, signer Signer) {
	r.signers[alg] = signer
}

// Get 获取算法
func (r *Registry) Get(alg Algorithm) (Signer, error) {
	signer, exists := r.signers[alg]
	if !exists {
		return nil, fmt.Errorf("algorithm %s not supported", alg)
	}
	return signer, nil
}

// List 列出所有支持的算法
func (r *Registry) List() []Algorithm {
	algs := make([]Algorithm, 0, len(r.signers))
	for alg := range r.signers {
		algs = append(algs, alg)
	}
	return algs
}
