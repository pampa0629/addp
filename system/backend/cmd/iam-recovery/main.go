package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/system/i18n"
	"github.com/google/uuid"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/iamcli"
	"github.com/pquerna/otp/totp"
	"golang.org/x/term"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "IAM Recovery 失败: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin *os.File, stdout *os.File) error {
	action, attemptID, submissionID, err := parseRecoveryCommand(args)
	if err != nil {
		return err
	}
	if !term.IsTerminal(int(stdout.Fd())) || (action == "apply" && !term.IsTerminal(int(stdin.Fd()))) {
		return fmt.Errorf("%s 必须直接运行在交互终端", action)
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

	if action == "prepare" {
		prepared, err := service.Prepare(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Attempt ID: %d\n", prepared.AttemptID)
		fmt.Fprintf(stdout, "Recovery Secret（仅显示一次，有效至 %s）:\n%s\n", prepared.ExpiresAt.Format(time.RFC3339), prepared.Secret)
		fmt.Fprintf(stdout, recoveryText("prepare_instructions")+"\n", prepared.AttemptID)
		return nil
	}
	if action == "status" {
		result, err := service.Resolve(ctx, attemptID, submissionID)
		printRecoveryResolution(stdout, result)
		if err != nil {
			return errors.New(recoveryText("state_unknown"))
		}
		return nil
	}
	return applyRecovery(ctx, service, attemptID, stdin, stdout)
}

func applyRecovery(
	ctx context.Context,
	service *iam.RecoveryService,
	attemptID int64,
	stdin *os.File,
	stdout *os.File,
) error {
	recoverySecret, err := iamcli.ReadHidden(stdin, stdout, "Recovery Secret: ")
	if err != nil {
		return err
	}
	validation, err := service.Validate(ctx, attemptID, recoverySecret)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Attempt ID: %d\n", validation.AttemptID)
	targets := validation.Targets
	reader := bufio.NewReader(stdin)
	inputs := make([]iam.RecoveryAdministratorInput, 0, len(targets))
	for _, target := range targets {
		roleKey := target.RoleKey
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
			PrincipalID: target.PrincipalID, RoleKey: roleKey, Password: password, TOTPSecret: key.Secret(), TOTPProofs: proofs,
		})
	}

	submissionID := uuid.New()
	if _, err := fmt.Fprintf(stdout, "Submission ID: %s\n", submissionID); err != nil {
		return err
	}
	result, err := service.Apply(ctx, iam.RecoveryApplyInput{
		AttemptID: attemptID, SubmissionID: submissionID, Targets: targets, RecoverySecret: recoverySecret, Administrators: inputs,
	})
	if err != nil {
		fmt.Fprintf(stdout, "addp-iam-recovery status %d %s\n", attemptID, submissionID)
		return errors.New(recoveryText("state_unknown"))
	}
	ids := make([]int64, 0, len(result.Principals))
	for id := range result.Principals {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	fmt.Fprintf(stdout, "\nIAM 管理员凭据恢复已完成于 %s（Attempt %d）\n", result.CompletedAt.Format(time.RFC3339), result.AttemptID)
	for _, id := range ids {
		fmt.Fprintf(stdout, "%s -> Principal %d\n", result.Principals[id], id)
	}
	fmt.Fprintln(stdout, "旧密码、旧 TOTP 和旧会话均已失效。请使用认证器新生成的验证码重新登录验证；原有暂停状态保持不变。")
	return nil
}

// Identifiers are non-secret correlation values; all credentials still use TTY.
func parseRecoveryCommand(args []string) (string, int64, uuid.UUID, error) {
	invalid := func() (string, int64, uuid.UUID, error) { return "", 0, uuid.Nil, errors.New(recoveryText("usage")) }
	if len(args) == 1 && args[0] == "prepare" {
		return "prepare", 0, uuid.Nil, nil
	}
	if len(args) < 2 || (args[0] != "apply" && args[0] != "status") {
		return invalid()
	}
	id, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != args[1] {
		return invalid()
	}
	if args[0] == "apply" && len(args) == 2 {
		return "apply", id, uuid.Nil, nil
	}
	if args[0] == "status" && len(args) == 3 {
		submission, err := uuid.Parse(args[2])
		if err == nil && submission != uuid.Nil && submission.String() == args[2] {
			return "status", id, submission, nil
		}
	}
	return invalid()
}

func recoveryText(key string) string {
	lang := commoni18n.LangZhCN
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LC_MESSAGES")
	}
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		lang = commoni18n.LangEn
	}
	return commoni18n.ForLanguage(lang, "system.iam.recovery_cli."+key)
}

func printRecoveryResolution(out *os.File, result *iam.RecoveryResolution) {
	fmt.Fprintf(out, "Attempt ID: %d\nstate=%s\n", result.AttemptID, result.State)
	if result.SubmissionID != uuid.Nil {
		fmt.Fprintf(out, "Submission ID: %s\n", result.SubmissionID)
	}
	if result.CompletedAt != nil {
		fmt.Fprintf(out, "completed_at=%s\n", result.CompletedAt.Format(time.RFC3339))
	}
	fmt.Fprintln(out, recoveryText("state_"+string(result.State)))
	ids := make([]int64, 0, len(result.Principals))
	for id := range result.Principals {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		fmt.Fprintf(out, "%s -> Principal %d; authorization_version=%d\n", result.Principals[id], id, result.AuthorizationVersions[id])
	}
}
