package iamcli

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/pquerna/otp/totp"
	"golang.org/x/term"
)

func ReadHidden(stdin *os.File, stdout *os.File, prompt string) (string, error) {
	fmt.Fprint(stdout, prompt)
	value, err := term.ReadPassword(int(stdin.Fd()))
	fmt.Fprintln(stdout)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(value)), nil
}

func ReadConfirmedPassword(
	stdin *os.File,
	stdout *os.File,
	firstPrompt string,
	confirmationPrompt string,
) (string, error) {
	password, err := ReadHidden(stdin, stdout, firstPrompt)
	if err != nil {
		return "", err
	}
	confirmation, err := ReadHidden(stdin, stdout, confirmationPrompt)
	if err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(confirmation)) != 1 {
		return "", fmt.Errorf("两次密码输入不一致")
	}
	return password, nil
}

func ReadConsecutiveTOTPProofs(
	reader *bufio.Reader,
	stdout io.Writer,
	secret string,
) ([]iam.BootstrapTOTPProof, error) {
	proofs := make([]iam.BootstrapTOTPProof, 0, 2)
	var previousCounter int64 = -1
	for len(proofs) < 2 {
		if len(proofs) == 0 {
			fmt.Fprint(stdout, "输入当前 TOTP 验证码: ")
		} else {
			fmt.Fprint(stdout, "等待验证码变化后输入下一个 TOTP 验证码: ")
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		code := strings.TrimSpace(line)
		if !IsTOTPCodeFormat(code) {
			fmt.Fprintln(stdout, "格式错误：请输入认证器生成的 6 位数字验证码，不要输入 TOTP Secret。")
			continue
		}
		verifiedAt := time.Now().UTC()
		counter, valid := MatchTOTP(secret, code, verifiedAt)
		if !valid {
			fmt.Fprintln(stdout, "验证码无效，请确认设备时间自动同步后重试。")
			continue
		}
		if previousCounter >= 0 && counter != previousCounter+1 {
			fmt.Fprintln(stdout, "必须使用紧邻的下一个时间窗口验证码，请重试。")
			continue
		}
		proofs = append(proofs, iam.BootstrapTOTPProof{Code: code, VerifiedAt: verifiedAt})
		previousCounter = counter
	}
	return proofs, nil
}

func IsTOTPCodeFormat(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func MatchTOTP(secret, code string, now time.Time) (int64, bool) {
	if !IsTOTPCodeFormat(code) {
		return 0, false
	}
	for _, offset := range []int{-1, 0, 1} {
		candidateTime := now.Add(time.Duration(offset*30) * time.Second)
		candidate, err := totp.GenerateCode(secret, candidateTime)
		if err == nil && subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return candidateTime.Unix() / 30, true
		}
	}
	return 0, false
}
