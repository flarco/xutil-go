package gxutil

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"

	pq "github.com/lib/pq"
	"github.com/spf13/cast"
)

// PostgresConn is a Postgres connection
type PostgresConn struct {
	BaseConn
	URL string
}

// Init initiates the object
func (conn *PostgresConn) Init() error {
	conn.BaseConn = BaseConn{
		URL:  conn.URL,
		Type: "postgres",
	}

	// Turn off Bulk export for now
	// the CopyToStdout function frequently produces error `read |0: file already closed`
	conn.BaseConn.SetProp("allow_bulk_export", "false")

	return conn.BaseConn.Init()
}

// CopyToStdout Copy TO STDOUT
func (conn *PostgresConn) CopyToStdout(sql string) (stdOutReader io.Reader, err error) {
	var stderr bytes.Buffer
	copyQuery := F(`\copy ( %s ) TO STDOUT WITH CSV HEADER DELIMITER ',' QUOTE '"' ESCAPE '"'`, sql)
	copyQuery = strings.ReplaceAll(copyQuery, "\n", " ")

	proc := exec.Command("psql", conn.URL, "-X", "-c", copyQuery)
	proc.Stderr = &stderr
	stdOutReader, err = proc.StdoutPipe()

	go func() {
		if proc.Run() != nil {
			// bytes, _ := proc.CombinedOutput()
			cmdStr := strings.ReplaceAll(strings.Join(proc.Args, " "), conn.URL, "$DBURL")
			println("COPY FROM Command -> ", cmdStr)
			println("COPY FROM Error   -> ", stderr.String())
		}
	}()

	return stdOutReader, err
}

// BulkExportStream uses the bulk dumping (COPY)
func (conn *PostgresConn) BulkExportStream(sql string) (ds Datastream, err error) {
	_, err = exec.LookPath("psql")
	if err != nil {
		Log("psql not found in path. Using cursor...")
		return conn.StreamRows(sql)
	}

	if !cast.ToBool(conn.BaseConn.GetProp("allow_bulk_export")) {
		return conn.StreamRows(sql)
	}

	stdOutReader, err := conn.CopyToStdout(sql)
	if err != nil {
		return ds, err
	}

	csv := CSV{Reader: stdOutReader}
	ds, err = csv.ReadStream()

	return ds, err
}

// BulkImportStream inserts a stream into a table
func (conn *PostgresConn) BulkImportStream(tableFName string, ds Datastream) (count uint64, err error) {

	columns := ds.GetFields()
	schema, table := splitTableFullName(tableFName)

	txn := conn.Db().MustBegin()

	stmt, err := txn.Prepare(pq.CopyInSchema(schema, table, columns...))
	if err != nil {
		return count, Error(err, fmt.Sprint(table, columns))
	}

	for row := range ds.Rows {
		count++
		// Do insert
		_, err := stmt.Exec(row...)
		if err != nil {
			txn.Rollback()
			return count, Error(err, "\n"+fmt.Sprint(row))
		}
	}

	_, err = stmt.Exec()
	if err != nil {
		txn.Rollback()
		return count, Error(err, "stmt.Exec()")
	}

	err = stmt.Close()
	if err != nil {
		return count, Error(err, "stmt.Close()")
	}

	err = txn.Commit()
	if err != nil {
		return count, Error(err, "txn.Commit()")
	}

	return count, nil
}
