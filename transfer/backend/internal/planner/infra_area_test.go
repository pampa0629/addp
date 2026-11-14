package planner

import (
	"testing"

	"github.com/addp/common/infra/objectstore"
	objectminio "github.com/addp/common/infra/objectstore/minio"
	sdk "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func testInfraArea(t *testing.T) objectstore.ContentArea {
	t.Helper()
	client, err := sdk.New("localhost:9000", &sdk.Options{Creds: credentials.NewStaticV4("test", "test-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	return objectminio.New(client, "manager")
}
