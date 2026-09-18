package config

import (
	"errors"
	commonconfig "github.com/addp/common/config"
	"net"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	commonconfig.BaseConfig
	Port                string
	SystemURL           string
	ServiceClientSecret string
	FalkorAddress       string
	FalkorPassword      string
}

func Load() (*Config, error) {
	commonconfig.LoadEnv()
	c := fromEnvironment()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func fromEnvironment() *Config {
	c := &Config{Port: commonconfig.GetEnv("ONTOLOGY_BACKEND_PORT", "8195"),
		SystemURL:           commonconfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
		ServiceClientSecret: os.Getenv("ONTOLOGY_SERVICE_CLIENT_SECRET"),
		FalkorAddress:       commonconfig.GetEnv("INFRA_FALKORDB_ADDRESS", "127.0.0.1:16479"),
		FalkorPassword:      os.Getenv("INFRA_FALKORDB_PASSWORD")}
	commonconfig.LoadDeploymentConfig(&c.BaseConfig)
	c.SystemServiceURL = c.SystemURL
	return c
}

func (c *Config) Validate() error {
	port, err := strconv.Atoi(c.Port)
	host, graphPort, graphErr := net.SplitHostPort(c.FalkorAddress)
	gp, gpErr := strconv.Atoi(graphPort)
	u, urlErr := url.Parse(c.SystemURL)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != c.Port || graphErr != nil || host == "" || gpErr != nil || gp < 1 || gp > 65535 ||
		urlErr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		len(c.ServiceClientSecret) < 32 || len(c.ServiceClientSecret) > 72 || c.FalkorPassword == "" || c.DBHost == "" || c.DBName == "" {
		return errors.New("invalid ontology deployment configuration")
	}
	return nil
}

func (c *Config) DatabaseDSN() string {
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.DBHost, c.DBPort), User: url.UserPassword(c.DBUser, c.DBPassword), Path: "/" + c.DBName}
	q := url.Values{"sslmode": {"disable"}, "search_path": {"ontology"}, "connect_timeout": {"5"}}
	u.RawQuery = q.Encode()
	return u.String()
}
