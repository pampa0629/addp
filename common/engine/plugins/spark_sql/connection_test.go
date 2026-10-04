package spark_sql

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

// Observe the actual SASL wire credentials across all Thrift consumers. The
// disposable peer rejects the handshake before opening any SQL session.
func TestSparkThriftConsumersUseDeclaredUsername(t *testing.T) {
	for _, operation := range []string{"probe", "query", "table session"} {
		t.Run(operation, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			observed := make(chan []string, 1)
			peerError := make(chan error, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					peerError <- err
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				read := func() ([]byte, error) {
					header := make([]byte, 5)
					if _, err := io.ReadFull(connection, header); err != nil {
						return nil, err
					}
					length := binary.BigEndian.Uint32(header[1:])
					if length > 4096 {
						return nil, fmt.Errorf("oversized SASL message")
					}
					body := make([]byte, length)
					_, err := io.ReadFull(connection, body)
					return body, err
				}
				mechanism, err := read()
				if err != nil {
					peerError <- err
					return
				}
				if string(mechanism) != "PLAIN" {
					peerError <- fmt.Errorf("mechanism %q", mechanism)
					return
				}
				payload, err := read()
				if err != nil {
					peerError <- err
					return
				}
				observed <- strings.Split(string(payload), "\x00")
				_, err = connection.Write([]byte{3, 0, 0, 0, 0})
				if err != nil {
					peerError <- err
				}
			}()
			info := plugin.ConnectionInfo{"host": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port,
				"username": "spark_fixture", "password": "fixture_password", "user": "undeclared_legacy_user"}
			spark := &SparkSQLPlugin{}
			switch operation {
			case "probe":
				err = spark.TestConnection(context.Background(), info)
			case "query":
				_, err = spark.ExecuteSQL(context.Background(), info, "SELECT 1", plugin.QueryOptions{})
			case "table session":
				spark.query = func(context.Context, plugin.ConnectionInfo, string) (*plugin.QueryResult, error) {
					return &plugin.QueryResult{Rows: []map[string]interface{}{{"col_name": "amount", "data_type": "bigint"}}}, nil
				}
				_, err = spark.OpenTableReadSession(context.Background(), info,
					plugin.TabularItemPath(7, plugin.EngineCatalogTermDatabase, "default", "orders"), plugin.TableReadSessionOptions{})
			}
			if err == nil {
				t.Fatal("fixture must reject the SASL handshake")
			}
			select {
			case fields := <-observed:
				if len(fields) != 3 || fields[1] != "spark_fixture" || fields[2] != "fixture_password" {
					t.Fatal("wire credentials do not match the declared username/password contract")
				}
			case err := <-peerError:
				t.Fatal(err)
			case <-time.After(6 * time.Second):
				t.Fatal("Thrift consumer did not reach fixture")
			}
		})
	}
}
