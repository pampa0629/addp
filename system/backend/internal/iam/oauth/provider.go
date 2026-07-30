package oauth

import (
	"errors"

	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"gorm.io/gorm"
)

type Provider struct {
	OAuth2   fosite.OAuth2Provider
	Storage  *Storage
	Strategy *Strategy
	Config   *fosite.Config
}

func NewProvider(
	db *gorm.DB,
	providerConfig ProviderConfig,
	strategyConfig StrategyConfig,
) (*Provider, error) {
	config, err := providerConfig.Fosite()
	if err != nil {
		return nil, err
	}
	storage, err := NewStorage(db, providerConfig.DevicePollingInterval)
	if err != nil {
		return nil, err
	}
	strategy, err := NewStrategy(strategyConfig, storage)
	if err != nil {
		return nil, err
	}
	if strategyConfig.AccessTokenLifespan != providerConfig.AccessTokenLifespan ||
		strategyConfig.RefreshTokenLifespan != providerConfig.RefreshTokenLifespan ||
		strategyConfig.AuthorizeCodeLifespan != providerConfig.AuthorizeCodeLifespan ||
		strategyConfig.DeviceCodeLifespan != providerConfig.DeviceCodeLifespan {
		return nil, errors.New("OAuth Provider 与 Strategy 生命周期配置不一致")
	}

	oauth2Provider := compose.Compose(
		config,
		storage,
		strategy,
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2ClientCredentialsGrantFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2TokenRevocationFactory,
		compose.OAuth2PKCEFactory,
		compose.RFC8628DeviceFactory,
		compose.RFC8628DeviceAuthorizationTokenFactory,
	)
	return &Provider{
		OAuth2:   oauth2Provider,
		Storage:  storage,
		Strategy: strategy,
		Config:   config,
	}, nil
}
