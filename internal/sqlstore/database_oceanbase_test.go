package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"testing"
)

const validOceanBaseURL = "mysql+aoceanbase://root%40tenant:secret@127.0.0.1:2881/powercontext?charset=utf8mb4"

type recordingDBTX struct{ statements []string }

func (r *recordingDBTX) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	r.statements = append(r.statements, query)
	return driver.RowsAffected(0), nil
}

func (*recordingDBTX) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("unexpected QueryContext")
}

func (*recordingDBTX) QueryRowContext(context.Context, string, ...any) *sql.Row {
	panic("unexpected QueryRowContext")
}

func TestOceanBaseDriverConfigPreservesFrozenURLContract(t *testing.T) {
	config, err := oceanBaseDriverConfig(validOceanBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if config.User != "root@tenant" || config.Passwd != "secret" || config.Addr != "127.0.0.1:2881" ||
		config.DBName != "powercontext" || config.Params["charset"] != "utf8mb4" || !config.ParseTime {
		t.Fatalf("config = %#v", config)
	}
}

func TestOceanBaseDriverConfigRejectsIncompleteOrWrongURLsWithoutLeakingSecrets(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"mysql+pymysql://root:secret@127.0.0.1:2881/powercontext?charset=utf8mb4",
		"mysql+aoceanbase://root:secret@127.0.0.1/powercontext?charset=utf8mb4",
		"mysql+aoceanbase://root:secret@127.0.0.1:2881/?charset=utf8mb4",
		"mysql+aoceanbase://root:secret@127.0.0.1:2881/powercontext",
		"mysql+aoceanbase://:secret@127.0.0.1:2881/powercontext?charset=utf8mb4",
		"mysql+aoceanbase://root:secret@127.0.0.1:0/powercontext?charset=utf8mb4",
		"mysql+aoceanbase://root:secret@127.0.0.1:65536/powercontext?charset=utf8mb4",
	} {
		err := ValidateOceanBaseURL(rawURL)
		if err == nil {
			t.Errorf("accepted %q", rawURL)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("error leaked password: %v", err)
		}
	}
}

func TestMySQLSchemaUsesPythonPayloadVariants(t *testing.T) {
	recorder := &recordingDBTX{}
	if err := EnsureBuiltinSchemaForDialect(t.Context(), recorder, MySQLDialect); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(recorder.statements, "\n")
	if !strings.Contains(joined, "payload MEDIUMBLOB NOT NULL") ||
		!strings.Contains(joined, "searchable_text MEDIUMTEXT") ||
		strings.Contains(joined, " payload BLOB") {
		t.Fatalf("unexpected MySQL schema:\n%s", joined)
	}
}
