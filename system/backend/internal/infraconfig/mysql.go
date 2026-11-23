package infraconfig

import (
	"context"
	"gorm.io/gorm"
)

type mysqlConfiguration struct{ db *gorm.DB }

func (m mysqlConfiguration) read(ctx context.Context) ([]Parameter, error) {
	var connections, zone, isolation string
	err := m.db.WithContext(ctx).Raw("SELECT @@GLOBAL.max_connections, @@GLOBAL.time_zone, @@GLOBAL.transaction_isolation").Row().Scan(&connections, &zone, &isolation)
	if err != nil {
		return nil, ErrUnavailable
	}
	return []Parameter{
		{Name: "max_connections", Value: connections, Activation: "online", Source: "unknown"},
		{Name: "time_zone", Value: zone, Activation: "new_connection", Source: "unknown"},
		{Name: "transaction_isolation", Value: isolation, Activation: "new_connection", Source: "unknown"},
	}, nil
}

func (mysqlConfiguration) apply(_ context.Context, name, _ string, _ func(string) error) (Outcome, error) {
	switch name {
	case "max_connections", "time_zone", "transaction_isolation":
		return Outcome{}, ErrReadOnly
	default:
		return Outcome{}, ErrNotFound
	}
}
