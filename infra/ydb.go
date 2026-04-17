package infra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"
)

type StreamSummary struct {
	SessionID  string
	Timestamp  time.Time
	Pattern    []byte
	MatchCount int64
}

type StreamSummaryWriter interface {
	BulkUpsertStreamSummary(ctx context.Context, summary StreamSummary) error
	Enabled() bool
	Close(ctx context.Context) error
}

type YDBClient struct {
	driver    *ydb.Driver
	tablePath string
	enabled   bool
}

func NewYDBClient(ctx context.Context, dsn string, tablePath string) (*YDBClient, error) {
	client := &YDBClient{
		tablePath: tablePath,
		enabled:   dsn != "" && tablePath != "",
	}
	if !client.enabled {
		return client, nil
	}

	driver, err := ydb.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}

	client.driver = driver
	if err := client.ensureSchema(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, err
	}

	return client, nil
}

func (c *YDBClient) Enabled() bool {
	return c != nil && c.enabled && c.driver != nil
}

func (c *YDBClient) BulkUpsertStreamSummary(ctx context.Context, summary StreamSummary) error {
	if !c.Enabled() {
		return nil
	}

	ts := summary.Timestamp.UTC()
	var rowFields [4]types.StructValueOption
	rowFields[0] = types.StructFieldValue("session_id", types.UTF8Value(summary.SessionID))
	rowFields[1] = types.StructFieldValue("timestamp", types.TimestampValueFromTime(ts))
	rowFields[2] = types.StructFieldValue("pattern", types.BytesValue(summary.Pattern))
	rowFields[3] = types.StructFieldValue("match_count", types.Int64Value(summary.MatchCount))

	row := types.StructValue(rowFields[:]...)
	bulkData := table.BulkUpsertDataRows(types.ListValue(row))
	tableClient := c.driver.Table()

	return tableClient.Do(ctx, func(ctx context.Context, _ table.Session) error {
		return tableClient.BulkUpsert(ctx, c.tablePath, bulkData)
	})
}

func (c *YDBClient) Close(ctx context.Context) error {
	if c == nil || c.driver == nil {
		return nil
	}

	c.enabled = false
	return c.driver.Close(ctx)
}

func (c *YDBClient) ensureSchema(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}

	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    session_id Utf8,
    timestamp Timestamp,
    pattern String,
    match_count Int64,
    PRIMARY KEY (session_id, timestamp)
);
`, quoteYDBPath(c.tablePath))

	return c.driver.Table().Do(ctx, func(ctx context.Context, s table.Session) error {
		return s.ExecuteSchemeQuery(ctx, query)
	})
}

func quoteYDBPath(path string) string {
	safe := strings.ReplaceAll(path, "`", "")
	return "`" + safe + "`"
}
