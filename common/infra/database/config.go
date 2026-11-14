// Package database describes a platform database connection supplied at startup.
package database

type Config struct {
	Host, Port, User, Password string
	DBName, Schema, SSLMode    string
}
