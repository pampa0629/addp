package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

func TestMonitorNotificationLeasesAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_MONITOR_DELIVERY") != "1" {
		t.Skip("requires the disposable Monitor MySQL gate")
	}
	driver, err := platformmysql.Dialector(database.Config{
		Host: os.Getenv("ADDP_TEST_MYSQL_HOST"), Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"), DBName: "monitor",
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, table := range []string{"webhook_deliveries", "email_deliveries"} {
		err := db.Exec("CREATE TABLE monitor." + table + ` (
          id BIGINT UNSIGNED PRIMARY KEY, delivery_id VARCHAR(36), status VARCHAR(20) NOT NULL,
          attempt_count INT NOT NULL, next_attempt_at DATETIME(6), claimed_by VARCHAR(255),
          lease_expires_at DATETIME(6), updated_at DATETIME(6), delivered_at DATETIME(6),
          last_http_status INT, last_error TEXT, secret_ciphertext TEXT
        ) ENGINE=InnoDB`).Error
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO monitor."+table+
			" (id,delivery_id,status,attempt_count,next_attempt_at) VALUES (1,'first','pending',0,?), (2,'second','pending',0,?)", now, now).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`CREATE TABLE monitor.platform_alert_deliveries (
      id VARCHAR(36) PRIMARY KEY, channel VARCHAR(20), status VARCHAR(20), attempt_count INT,
      claim_id VARCHAR(36), next_attempt_at DATETIME(6), lease_expires_at DATETIME(6), last_error TEXT, secret_ciphertext TEXT
    ) ENGINE=InnoDB`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO monitor.platform_alert_deliveries (id,channel,status,attempt_count,next_attempt_at) VALUES ('a','email','pending',0,?),('b','webhook','pending',1,?)", now, now).Error; err != nil {
		t.Fatal(err)
	}
	t.Run("skip locked and fence previous webhook lease", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if err := tx.Exec("SELECT id FROM monitor.webhook_deliveries WHERE id=1 FOR UPDATE").Error; err != nil {
			t.Fatal(err)
		}
		first := NewWebhookDispatcher(db, nil, WebhookDispatcherConfig{WorkerID: "first-worker", LeaseDuration: time.Minute})
		second := NewWebhookDispatcher(db, nil, WebhookDispatcherConfig{WorkerID: "second-worker", LeaseDuration: time.Minute})
		row, err := first.claimDue(ctx, now)
		if err != nil || row == nil || row.ID != 2 || row.DeliveryID != "second" || row.AttemptCount != 1 || row.ClaimedBy != "first-worker" {
			t.Fatalf("skip locked: row=%+v err=%v", row, err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		another, err := second.claimDue(ctx, now)
		if err != nil || another == nil || another.ID != 1 {
			t.Fatalf("released lock: row=%+v err=%v", another, err)
		}
		if err := second.finishAttempt(ctx, *another, WebhookSendResult{HTTPStatus: 200}, nil, now); err != nil {
			t.Fatal(err)
		}
		if next, err := second.claimDue(ctx, now); err != nil || next != nil {
			t.Fatalf("active lease taken: %+v %v", next, err)
		}
		recovered, err := second.claimDue(ctx, now.Add(time.Minute))
		if err != nil || recovered == nil || recovered.ID != 2 || recovered.AttemptCount != 2 {
			t.Fatalf("expired lease: row=%+v err=%v", recovered, err)
		}
		if err := first.finishAttempt(ctx, *row, WebhookSendResult{HTTPStatus: 200}, nil, now); err == nil {
			t.Fatal("superseded worker completed webhook")
		}
		if err := second.finishAttempt(ctx, *recovered, WebhookSendResult{HTTPStatus: 200}, nil, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("email lease and completion", func(t *testing.T) {
		d := NewEmailDispatcher(db, nil, EmailDispatcherConfig{WorkerID: "email-worker", LeaseDuration: time.Minute})
		row, err := d.claimDue(t.Context(), now)
		if err != nil || row == nil || row.DeliveryID != "first" || row.Status != "delivering" || row.AttemptCount != 1 {
			t.Fatalf("claim: %+v %v", row, err)
		}
		if err := d.finishAttempt(t.Context(), *row, nil, now); err != nil {
			t.Fatal(err)
		}
		var stored models.EmailDelivery
		if err := db.First(&stored, row.ID).Error; err != nil || stored.Status != "delivered" || stored.LeaseExpiresAt != nil {
			t.Fatalf("completion: %+v %v", stored, err)
		}
	})
	t.Run("platform channel availability and retry budget", func(t *testing.T) {
		// A consumed retry budget exercises the terminal path before any network send.
		n := &PlatformAlertService{db: db, lease: time.Minute, maxAttempts: 1}
		processed, err := n.DispatchOnce(t.Context(), now)
		if err != nil || !processed {
			t.Fatalf("dispatch=%v err=%v", processed, err)
		}
		var rows []models.PlatformAlertDelivery
		if err := db.Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].Status != "pending" || rows[0].AttemptCount != 0 || rows[1].Status != "dead" || rows[1].AttemptCount != 2 || rows[1].LastError != "notification_attempt_limit" {
			t.Fatalf("channel selection or retry budget: %+v", rows)
		}
	})
}
