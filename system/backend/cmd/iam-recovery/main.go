package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/iamcli"
	"github.com/pquerna/otp/totp"
	"golang.org/x/term"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var requiredRoles = []string{
	"platform.system_administrator",
	"platform.security_administrator",
	"platform.audit_administrator",
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "IAM Recovery 失败: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin *os.File, stdout *os.File) error {
	if len(args) != 1 || (args[0] != "prepare" && args[0] != "apply") {
		return errors.New("用法: iam-recovery prepare | iam-recovery apply")
	}
	if !term.IsTerminal(int(stdout.Fd())) || (args[0] == "apply" && !term.IsTerminal(int(stdin.Fd()))) {
		return fmt.Errorf("%s 必须直接运行在交互终端", args[0])
	}

	commonconfig.LoadEnv()
	cfg := config.Load()
	db, err := gorm.Open(postgres.Open(cfg.PostgreSQLDSN()), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("连接 PostgreSQL: %w", err)
	}
	if err := iamcli.RequireCurrentMigration(db); err != nil {
		return err
	}
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		return err
	}
	service, err := iam.NewRecoveryService(iam.NewRepository(db), cipher, time.Hour, nil, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if args[0] == "prepare" {
		secret, expiresAt, err := service.Prepare(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Recovery Secret（仅显示一次，有效至 %s）:\n%s\n", expiresAt.Format(time.RFC3339), secret)
		fmt.Fprintln(stdout, "不要截图、写入文件或发送该 Secret。现在请直接运行 addp-iam-recovery apply。")
		return nil
	}
	return applyRecovery(ctx, service, stdin, stdout)
}

func applyRecovery(
	ctx context.Context,
	service *iam.RecoveryService,
	stdin *os.File,
	stdout *os.File,
) error {
	recoverySecret, err := iamcli.ReadHidden(stdin, stdout, "Recovery Secret: ")
	if err != nil {
		return err
	}
	targets, err := service.Validate(ctx, recoverySecret)
	if err != nil {
		return err
	}
	targetByRole := make(map[string]iam.RecoveryAdministratorTarget, len(targets))
	for _, target := range targets {
		targetByRole[target.RoleKey] = target
	}

	reader := bufio.NewReader(stdin)
	inputs := make([]iam.RecoveryAdministratorInput, 0, len(requiredRoles))
	for _, roleKey := range requiredRoles {
		target := targetByRole[roleKey]
		fmt.Fprintf(stdout, "\n恢复 %s（%s，账号 %s）\n", target.DisplayName, roleKey, target.Username)
		password, err := iamcli.ReadConfirmedPassword(
			stdin, stdout, "输入新密码（至少 14 字符）: ", "再次输入新密码: ",
		)
		if err != nil {
			return fmt.Errorf("%s: %w", roleKey, err)
		}
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "ADDP", AccountName: target.Username})
		if err != nil {
			return fmt.Errorf("生成 %s 的 TOTP: %w", roleKey, err)
		}
		fmt.Fprintln(stdout, "请使用认证器的“扫描二维码”入口扫描下面的新 TOTP 二维码：")
		if err := iamcli.PrintQRCode(stdout, key.URL()); err != nil {
			return fmt.Errorf("显示 %s 的 TOTP 二维码: %w", roleKey, err)
		}
		fmt.Fprintf(stdout, "认证器明确支持“手动输入设置密钥/TOTP”时，可使用备用 TOTP Secret: %s\n", key.Secret())
		fmt.Fprintln(stdout, "不要把 TOTP Secret 输入短信/邮件激活入口。")
		fmt.Fprintln(stdout, "请在对应管理员的认证器中新增条目；下面只输入认证器显示的 6 位验证码。")
		proofs, err := iamcli.ReadConsecutiveTOTPProofs(reader, stdout, key.Secret())
		if err != nil {
			return fmt.Errorf("验证 %s 的 TOTP: %w", roleKey, err)
		}
		inputs = append(inputs, iam.RecoveryAdministratorInput{
			RoleKey: roleKey, Password: password, TOTPSecret: key.Secret(), TOTPProofs: proofs,
		})
	}

	result, err := service.Apply(ctx, iam.RecoveryApplyInput{
		RecoverySecret: recoverySecret, Administrators: inputs,
	})
	if err != nil {
		return err
	}
	roles := make([]string, 0, len(result.Principals))
	for roleKey := range result.Principals {
		roles = append(roles, roleKey)
	}
	sort.Strings(roles)
	fmt.Fprintf(stdout, "\nIAM 三员凭据恢复已完成于 %s（Attempt %d）\n", result.CompletedAt.Format(time.RFC3339), result.AttemptID)
	for _, roleKey := range roles {
		fmt.Fprintf(stdout, "%s -> Principal %d\n", roleKey, result.Principals[roleKey])
	}
	fmt.Fprintln(stdout, "旧密码、旧 TOTP 和旧会话均已失效。请全量重启 ADDP 后分别验证三员登录。")
	return nil
}
