// Package database describes a platform database connection supplied at startup.
package database

import "time"

type Config struct {
	Host, Port, User, Password string
	DBName, Schema, SSLMode    string
	ConnectTimeout             time.Duration
}
