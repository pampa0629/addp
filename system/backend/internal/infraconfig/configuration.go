package infraconfig

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	ErrNotFound    = errors.New("unknown infrastructure parameter")
	ErrInvalid     = errors.New("invalid infrastructure parameter value")
	ErrReadOnly    = errors.New("infrastructure parameter is read only")
	ErrUnavailable = errors.New("infrastructure configuration unavailable")
)

type Parameter struct {
	Name           string `json:"name"`
	Value          string `json:"value"`
	Unit           string `json:"unit"`
	Source         string `json:"source"`
	Activation     string `json:"activation"`
	Editable       bool   `json:"editable"`
	PendingRestart bool   `json:"pending_restart"`
}

type Component struct {
	ID         string      `json:"id"`
	Provider   string      `json:"provider"`
	Status     string      `json:"status"`
	ObservedAt *time.Time  `json:"observed_at,omitempty"`
	Parameters []Parameter `json:"parameters"`
}

type Outcome struct {
	PreviousValue  string     `json:"previous_value"`
	RequestedValue string     `json:"requested_value"`
	Current        *Parameter `json:"current,omitempty"`
	Application    string     `json:"application" enums:"confirmed,unconfirmed"`
	Persistence    string     `json:"persistence" enums:"confirmed,unconfirmed,not_attempted"`
	FailureStage   string     `json:"failure_stage,omitempty" enums:"write,reload,persist,verify"`
	AuditRecorded  bool       `json:"audit_recorded"`
}

type componentBackend interface {
	read(context.Context) ([]Parameter, error)
	apply(context.Context, string, string, func(string) error) (Outcome, error)
}

type binding struct {
	id, provider, endpoint string
	backend                componentBackend
}

type Configuration struct {
	bindings []binding
}

func New(cfg *config.Config, runtimeDB *gorm.DB) *Configuration {
	database := binding{id: "database", provider: cfg.InfraDatabaseProvider}
	if database.provider == "postgres" && cfg.InfraPGConfigPassword != "" {
		management := *cfg
		management.PostgresUser = "addp_infra_config"
		management.PostgresPassword = cfg.InfraPGConfigPassword
		database.endpoint = net.JoinHostPort(cfg.PostgresHost, cfg.PostgresPort)
		database.backend = postgresConfiguration{dsn: management.PostgreSQLDSN()}
	} else if database.provider == "mysql" {
		database.backend = mysqlConfiguration{db: runtimeDB}
	}

	cache := binding{id: "redis", provider: "redis", endpoint: net.JoinHostPort(cfg.RedisHost, cfg.RedisPort)}
	if cfg.InfraRedisConfigPassword != "" {
		cache.backend = redisBinding{options: redis.Options{
			Addr: cache.endpoint, Username: "addp_infra_config", Password: cfg.InfraRedisConfigPassword,
			Protocol: 2, DisableIdentity: true, MaxRetries: -1,
			DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, ContextTimeoutEnabled: true,
		}}
	}
	return &Configuration{bindings: []binding{database, cache}}
}

func (s *Configuration) Read(ctx context.Context) []Component {
	result := make([]Component, 0, len(s.bindings))
	for _, target := range s.bindings {
		component := Component{ID: target.id, Provider: target.provider, Status: "unbound", Parameters: []Parameter{}}
		if target.backend != nil {
			readContext, cancel := context.WithTimeout(ctx, 2*time.Second)
			parameters, err := target.backend.read(readContext)
			cancel()
			component.Status = "unavailable"
			if err == nil {
				now := time.Now().UTC()
				component.Status, component.ObservedAt, component.Parameters = "available", &now, parameters
			}
		} else if target.id == "database" && target.provider != "postgres" {
			component.Status = "unsupported"
		}
		result = append(result, component)
	}
	return result
}

func (s *Configuration) Apply(ctx context.Context, id, name, value string, record func(endpoint, previous string) error) (Outcome, error) {
	for _, target := range s.bindings {
		if target.id != id {
			continue
		}
		if target.backend == nil {
			return Outcome{}, ErrUnavailable
		}
		return target.backend.apply(ctx, name, value, func(previous string) error {
			return record(target.endpoint, previous)
		})
	}
	return Outcome{}, ErrNotFound
}

func intValue(value string, min, max int64) (string, error) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < min || number > max {
		return "", ErrInvalid
	}
	return strconv.FormatInt(number, 10), nil
}
