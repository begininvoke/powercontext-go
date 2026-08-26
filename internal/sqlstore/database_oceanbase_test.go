package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"strconv"
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
		config.DBName != "powercontext" || !strings.Contains(config.FormatDSN(), "charset=utf8mb4") ||
		len(config.Params) != 0 || !config.ParseTime || !config.AllowNativePasswords {
		t.Fatal("OceanBase driver config does not preserve the frozen URL semantics")
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
		!strings.Contains(joined, "`cursor` MEDIUMBLOB NOT NULL") ||
		!strings.Contains(joined, "searchable_text MEDIUMTEXT") ||
		!strings.Contains(joined, "scope_id VARCHAR(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL") ||
		!strings.Contains(joined, "source_id VARCHAR(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL") ||
		!strings.Contains(joined, "source_type VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL") ||
		strings.Contains(joined, " payload BLOB") {
		t.Fatalf("unexpected MySQL schema:\n%s", joined)
	}
}

func TestEveryCoreMySQLVarcharUsesBinaryIdentityCollation(t *testing.T) {
	recorder := &recordingDBTX{}
	if err := EnsureBuiltinSchemaForDialect(t.Context(), recorder, MySQLDialect); err != nil {
		t.Fatal(err)
	}
	assertEveryMySQLVarcharIsBinary(t, recorder.statements)
}

func TestEveryHandoffReportMySQLVarcharUsesBinaryIdentityCollation(t *testing.T) {
	statements := make([]string, len(handoffReportSchema))
	for index, statement := range handoffReportSchema {
		statements[index] = mysqlIdentityColumns(statement)
	}
	assertEveryMySQLVarcharIsBinary(t, statements)
}

func assertEveryMySQLVarcharIsBinary(t *testing.T, statements []string) {
	t.Helper()
	for _, statement := range statements {
		for _, line := range strings.Split(statement, "\n") {
			if strings.Contains(line, "VARCHAR(") &&
				!strings.Contains(line, " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin") {
				t.Errorf("MySQL identity column lacks binary collation: %s", strings.TrimSpace(line))
			}
		}
	}
}

func TestOceanBaseQuotesCursorWithoutChangingCompatibleTableNames(t *testing.T) {
	statement := quoteCursorIdentifier("SELECT cursor FROM pc_source_cursors WHERE cursor > ? ORDER BY cursor")
	if statement != "SELECT `cursor` FROM pc_source_cursors WHERE `cursor` > ? ORDER BY `cursor`" {
		t.Fatalf("quoted statement = %q", statement)
	}
}

func TestEveryMySQLUTF8MB4KeyStaysBelowInnoDBLimit(t *testing.T) {
	recorder := &recordingDBTX{}
	if err := EnsureBuiltinSchemaForDialect(t.Context(), recorder, MySQLDialect); err != nil {
		t.Fatal(err)
	}
	columnPattern := regexp.MustCompile(`(?m)^\s*([a-z_]+)\s+(VARCHAR\((\d+)\)|BIGINT|INTEGER|DATE)`)
	constraintPattern := regexp.MustCompile(`(?is)(?:PRIMARY KEY|UNIQUE|FOREIGN KEY)\s*\(([^)]*)\)`)
	maximum := 0
	constraintCount := 0
	for _, statement := range recorder.statements {
		budgets := make(map[string]int)
		for _, match := range columnPattern.FindAllStringSubmatch(statement, -1) {
			switch {
			case match[3] != "":
				characters, err := strconv.Atoi(match[3])
				if err != nil {
					t.Fatal(err)
				}
				budgets[match[1]] = characters * 4
			case match[2] == "BIGINT":
				budgets[match[1]] = 8
			case match[2] == "INTEGER":
				budgets[match[1]] = 4
			case match[2] == "DATE":
				budgets[match[1]] = 3
			}
		}
		for _, match := range constraintPattern.FindAllStringSubmatch(statement, -1) {
			constraintCount++
			total := 0
			for _, raw := range strings.Split(match[1], ",") {
				name := strings.Trim(strings.TrimSpace(raw), "`")
				budget, ok := budgets[name]
				if !ok {
					t.Fatalf("unbudgeted key column %q in %q", name, match[0])
				}
				total += budget
			}
			if total >= 3072 {
				t.Fatalf("InnoDB key budget = %d for %q", total, match[0])
			}
			maximum = max(maximum, total)
		}
	}
	if constraintCount == 0 || maximum != 2560 {
		t.Fatalf("constraints = %d, maximum key budget = %d", constraintCount, maximum)
	}
}

func TestOceanBaseProfileRequiresMySQLTenant(t *testing.T) {
	tests := []struct {
		name, mode string
		queryErr   error
		valid      bool
	}{
		{name: "ob_compatibility_mode", mode: "MYSQL", valid: true},
		{name: "ob_compatibility_mode", mode: "mysql", valid: true},
		{name: "ob_compatibility_mode", mode: "ORACLE"},
		{name: "wrong", mode: "MYSQL"},
		{queryErr: sql.ErrNoRows},
	}
	for _, test := range tests {
		err := validateOceanBaseTenantMode(test.name, test.mode, test.queryErr)
		if test.valid && err != nil {
			t.Fatalf("valid tenant rejected: %v", err)
		}
		if !test.valid {
			var unsupported *UnsupportedOceanBaseTenantError
			if !errors.As(err, &unsupported) {
				t.Fatalf("tenant %q/%q error = %v", test.name, test.mode, err)
			}
		}
	}
}
