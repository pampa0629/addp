package main

import (
	"bufio"
	"context"
	"crypto/subtle"
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
	"github.com/addp/system/internal/migration"
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
	if err := requireMigrationVersion(db); err != nil {
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

func requireMigrationVersion(db *gorm.DB) error {
	catalog, err := migration.ReadCatalog(migration.EmbeddedSQL, migration.DefaultMigrationsRoot)
	if err != nil {
		return fmt.Errorf("读取内嵌 IAM migration 目录: %w", err)
	}
	var version uint
	var dirty bool
	if err := db.Raw(`SELECT version, dirty FROM system.schema_migrations`).Row().Scan(&version, &dirty); err != nil {
		return fmt.Errorf("读取 IAM migration 状态: %w", err)
	}
	if dirty || version != catalog.LatestVersion {
		return fmt.Errorf("IAM migration 必须为 (%d, clean)，当前为 (%d, dirty=%t)", catalog.LatestVersion, version, dirty)
	}
	return nil
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
	bootstrapSecret, err := readHidden(stdin, stdout, "Bootstrap Secret: ")
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
		password, err := readHidden(stdin, stdout, "输入独立密码（至少 14 字符）: ")
		if err != nil {
			return err
		}
		confirmation, err := readHidden(stdin, stdout, "再次输入密码: ")
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(password), []byte(confirmation)) != 1 {
			return fmt.Errorf("%s 的两次密码输入不一致", roleKey)
		}
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "ADDP", AccountName: administrator.Username})
		if err != nil {
			return fmt.Errorf("生成 %s 的 TOTP: %w", roleKey, err)
		}
		fmt.Fprintf(stdout, "TOTP Secret: %s\nEnrollment URI: %s\n", key.Secret(), key.URL())
		fmt.Fprintln(stdout, "请先将 TOTP Secret 或 Enrollment URI 添加到认证器 App。")
		fmt.Fprintln(stdout, "下面只接受认证器当前显示的 6 位数字验证码，不要输入 TOTP Secret。")
		proofs, err := readConsecutiveTOTPProofs(reader, stdout, key.Secret())
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

func readHidden(stdin *os.File, stdout *os.File, prompt string) (string, error) {
	fmt.Fprint(stdout, prompt)
	value, err := term.ReadPassword(int(stdin.Fd()))
	fmt.Fprintln(stdout)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(value)), nil
}

func readConsecutiveTOTPProofs(
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
		if !isTOTPCodeFormat(code) {
			fmt.Fprintln(stdout, "格式错误：请输入认证器生成的 6 位数字验证码，不要输入 TOTP Secret。")
			continue
		}
		verifiedAt := time.Now().UTC()
		counter, valid := matchTOTP(secret, code, verifiedAt)
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

func isTOTPCodeFormat(code string) bool {
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

func matchTOTP(secret, code string, now time.Time) (int64, bool) {
	if !isTOTPCodeFormat(code) {
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
