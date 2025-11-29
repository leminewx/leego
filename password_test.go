package leego

import "testing"

func TestPassword(t *testing.T) {
	pwd, err := HashPasswordByBcrypt([]byte("1234567890abc"))
	if err != nil {
		t.Error(err)
	}
	t.Log(string(pwd))

	if !VerifyPasswordByBcrypt(pwd, []byte("1234567890abc")) {
		t.Error("密码验证失败")
	} else {
		t.Log("密码验证成功")
	}

}
