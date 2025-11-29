package leego

import "golang.org/x/crypto/bcrypt"

// HashPasswordByBcrypt 加密密码
func HashPasswordByBcrypt(password []byte) ([]byte, error) {
	return bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
}

// VerifyPasswordByBcrypt 验证密码
func VerifyPasswordByBcrypt(hashedPassword, originalPassword []byte) bool {
	return bcrypt.CompareHashAndPassword(hashedPassword, originalPassword) == nil
}
