package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
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

type administratorManifest struct {
	RoleKey      string  `json:"role_key"`
	Username     string  `json:"username"`
	DisplayName  string  `json:"display_name"`
	PrimaryEmail *string `json:"primary_email,omitempty"`
	Locale       *string `json:"locale,omitempty"`
}

type bootstrapManifest struct {
	Administrators []administratorManifest `json:"administrators"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "IAM Bootstrap 失败: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin *os.File, stdout *os.File) error {
	if len(args) == 0 {
		return errors.New("用法: iam-bootstrap prepare | iam-bootstrap apply --manifest <path>")
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
	repository := iam.NewRepository(db)
	identityService := iam.NewIdentityService(repository, nil)
	cipher, err := iam.NewMFACredentialCipher(cfg.IAMMFAEncryptionKey)
	if err != nil {
		return err
	}
	service, err := iam.NewBootstrapService(repository, identityService, cipher, time.Hour, nil, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	switch args[0] {
	case "prepare":
		if len(args) != 1 {
			return errors.New("prepare 不接受参数")
		}
		if !term.IsTerminal(int(stdout.Fd())) {
			return errors.New("prepare 必须直接运行在终端，禁止重定向 Bootstrap Secret")
		}
		secret, expiresAt, err := service.Prepare(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Bootstrap Secret（仅显示一次，有效至 %s）:\n%s\n", expiresAt.Format(time.RFC3339), secret)
		return nil
	case "apply":
		flags := flag.NewFlagSet("apply", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		manifestPath := flags.String("manifest", "", "三员实名 Manifest")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || strings.TrimSpace(*manifestPath) == "" {
			return errors.New("用法: iam-bootstrap apply --manifest <path>")
		}
		if !term.IsTerminal(int(stdin.Fd())) || !term.IsTerminal(int(stdout.Fd())) {
			return errors.New("apply 必须直接运行在交互终端")
		}
		manifest, err := readManifest(*manifestPath)
		if err != nil {
			return err
		}
		return applyBootstrap(ctx, service, manifest, stdin, stdout)
	default:
		return fmt.Errorf("未知子命令 %q", args[0])
	}
}

func readManifest(path string) (*bootstrapManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 Manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest bootstrapManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("解析 Manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("Manifest 只能包含一个 JSON object")
	}
	if len(manifest.Administrators) != len(requiredRoles) {
		return nil, errors.New("Manifest 必须包含三个管理员")
	}
	return &manifest, nil
}

func applyBootstrap(
	ctx context.Context,
	service *iam.BootstrapService,
	manifest *bootstrapManifest,
	stdin *os.File,
	stdout *os.File,
) error {
	reader := bufio.NewReader(stdin)
	bootstrapSecret, err := iamcli.ReadHidden(stdin, stdout, "Bootstrap Secret: ")
	if err != nil {
		return err
	}
	manifestByRole := make(map[string]administratorManifest, len(manifest.Administrators))
	for _, administrator := range manifest.Administrators {
		if _, exists := manifestByRole[administrator.RoleKey]; exists {
			return fmt.Errorf("Manifest 包含重复角色 %q", administrator.RoleKey)
		}
		manifestByRole[administrator.RoleKey] = administrator
	}
	inputs := make([]iam.BootstrapAdministratorInput, 0, len(requiredRoles))
	for _, roleKey := range requiredRoles {
		administrator, exists := manifestByRole[roleKey]
		if !exists {
			return fmt.Errorf("Manifest 缺少角色 %q", roleKey)
		}
		fmt.Fprintf(stdout, "\n配置 %s（%s）\n", administrator.DisplayName, roleKey)
		password, err := iamcli.ReadConfirmedPassword(
			stdin, stdout, "输入独立密码（至少 14 字符）: ", "再次输入密码: ",
		)
		if err != nil {
			return fmt.Errorf("%s: %w", roleKey, err)
		}
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "ADDP", AccountName: administrator.Username})
		if err != nil {
			return fmt.Errorf("生成 %s 的 TOTP: %w", roleKey, err)
		}
		fmt.Fprintln(stdout, "请使用认证器的“扫描二维码”入口扫描下面的 TOTP 二维码：")
		if err := iamcli.PrintQRCode(stdout, key.URL()); err != nil {
			return fmt.Errorf("显示 %s 的 TOTP 二维码: %w", roleKey, err)
		}
		fmt.Fprintf(stdout, "认证器明确支持“手动输入设置密钥/TOTP”时，可使用备用 TOTP Secret: %s\n", key.Secret())
		fmt.Fprintln(stdout, "不要把 TOTP Secret 输入短信/邮件激活入口。")
		fmt.Fprintln(stdout, "下面只接受认证器当前显示的 6 位数字验证码，不要输入 TOTP Secret。")
		proofs, err := iamcli.ReadConsecutiveTOTPProofs(reader, stdout, key.Secret())
		if err != nil {
			return fmt.Errorf("验证 %s 的 TOTP: %w", roleKey, err)
		}
		inputs = append(inputs, iam.BootstrapAdministratorInput{
			RoleKey: roleKey, Username: administrator.Username, Password: password,
			DisplayName: administrator.DisplayName, PrimaryEmail: administrator.PrimaryEmail,
			Locale: administrator.Locale, TOTPSecret: key.Secret(), TOTPProofs: proofs,
		})
	}
	result, err := service.Apply(ctx, iam.BootstrapApplyInput{
		BootstrapSecret: bootstrapSecret, Administrators: inputs,
	})
	if err != nil {
		return err
	}
	roles := make([]string, 0, len(result.PrincipalIDs))
	for role := range result.PrincipalIDs {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	fmt.Fprintf(stdout, "\nIAM Bootstrap 已永久完成于 %s\n", result.CompletedAt.Format(time.RFC3339))
	for _, role := range roles {
		fmt.Fprintf(stdout, "%s -> Principal %d\n", role, result.PrincipalIDs[role])
	}
	return nil
}
