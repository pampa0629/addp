package main

import (
	"fmt"
	"log"

	"github.com/addp/portal/internal/api"
	"github.com/addp/portal/internal/config"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.LoadConfig()

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}

	router := api.SetupRouter(cfg, redisClient)

	addr := ":" + cfg.Port
	log.Printf("Portal BFF service starting on %s", addr)

	if err := router.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
