package main

import (
	"bufio"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	g "github.com/flarco/gxutil"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type testDB struct {
	name  string
	URL   string
	table string
	conn  g.Connection
}

var (
	testFile1Bytes []byte
)

var DBs = []*testDB{
	&testDB{
		// https://github.com/lib/pq
		name:  "Postgres",
		URL:   os.Getenv("POSTGRES_URL"),
		table: "public.test1",
	},

	// &testDB{
	// 	// https://github.com/mattn/go-sqlite3
	// 	name:  "SQLite",
	// 	URL:   "file:./test.db",
	// 	table: "main.test1",
	// },

	// &testDB{
	// 	// https://github.com/godror/godror
	// 	name:  "Oracle",
	// 	URL:   os.Getenv("ORACLE_URL"),
	// 	table: "system.test1",
	// },

	// &testDB{
	// 	// https://github.com/denisenkom/go-mssqldb
	// 	name:  "MySQL",
	// 	URL:   os.Getenv("MYSQL_URL"),
	// 	table: "mysql.test1",
	// },

	// &testDB{
	// 	// https://github.com/denisenkom/go-mssqldb
	// 	name:  "SQLServer",
	// 	URL:   os.Getenv("SQLSERVER_URL"),
	// 	table: "public.test1",
	// },

	// &testDB{
	// 	// https://github.com/snowflakedb/gosnowflake
	// 	name:  "Snowflake",
	// 	URL:   os.Getenv("SNOWFLAKE_URL"),
	// 	table: "public.test1",
	// },

	&testDB{
		// https://github.com/lib/pq
		name:  "Redshift",
		URL:   os.Getenv("REDSHIFT_URL"),
		table: "public.test1",
	},
}

func init() {
	// Log as JSON instead of the default ASCII formatter.
	log.SetFormatter(&log.JSONFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stderr)

	// Only log the warning severity or above.
	log.SetLevel(log.WarnLevel)

	for _, db := range DBs {
		if db.URL == "" {
			log.Fatal("No Env Var URL for " + db.name)
		} else if db.name == "SQLite" {
			os.Remove(strings.ReplaceAll(db.URL, "file:", ""))
		}
	}
}

func TestInToDb(t *testing.T) {
	csvFile := "tests/test1.csv"
	testFile1, err := os.Open(csvFile)
	if err != nil {
		assert.NoError(t, err)
		return
	}

	tReader, err := g.Decompress(bufio.NewReader(testFile1))
	assert.NoError(t, err)
	testFile1Bytes, err = ioutil.ReadAll(tReader)
	testFile1.Close()

	for _, tgtDB := range DBs {
		println(g.F("\n >> Tranferring from CSV(%s) to %s", csvFile, tgtDB.name))
		testFile1, err := os.Open(csvFile) // need to reopen each loop
		assert.NoError(t, err)

		cfg := Config{
			file:     testFile1,
			tgtDB:    tgtDB.URL,
			tgtTable: tgtDB.table,
			drop:     true,
			s3Bucket: os.Getenv("S3_BUCKET"),
		}
		err = runFileToDB(cfg)
		if err != nil {
			assert.NoError(t, err)
			return
		}
	}
}

func TestDbToDb(t *testing.T) {
	var err error
	assert.NoError(t, err)

	for _, srcDB := range DBs {
		for _, tgtDB := range DBs {
			if srcDB.name == "SQLite" && tgtDB.name == "SQLite" {
				continue
			}
			println(g.F("\n >> Tranferring from %s to %s", srcDB.name, tgtDB.name))
			cfg := Config{
				srcDB:    srcDB.URL,
				srcTable: srcDB.table,
				tgtDB:    tgtDB.URL,
				tgtTable: tgtDB.table + "_copy",
				drop:     true,
				s3Bucket: os.Getenv("S3_BUCKET"),
			}
			err = runDbToDb(cfg)
			if err != nil {
				assert.NoError(t, err)
				return
			}
		}
	}
}

func TestDbToOut(t *testing.T) {

	for _, srcDB := range DBs {
		filePath2 := g.F("tests/%s.out.csv", srcDB.name)
		println(g.F("\n >> Tranferring from %s to CSV (%s)", srcDB.name, filePath2))
		testFile2, err := os.Create(filePath2)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		srcTable := srcDB.table
		srcTableCopy := srcDB.table + "_copy"
		cfg := Config{
			srcDB:    srcDB.URL,
			srcTable: srcTable,
			file:     testFile2,
			drop:     true,
			s3Bucket: os.Getenv("S3_BUCKET"),
		}
		err = runDbToFile(cfg)
		if err != nil {
			assert.NoError(t, err)
			return
		}

		testFile2, err = os.Open(filePath2)
		assert.NoError(t, err)
		testFile2Bytes, err := ioutil.ReadAll(testFile2)

		if srcDB.name != "SQLite" {
			// SQLite uses int for bool, so it will not match
			equal := assert.Equal(t, string(testFile1Bytes), string(testFile2Bytes))

			if equal {
				err = os.Remove(filePath2)
				assert.NoError(t, err)

				conn := g.GetConn(srcDB.URL)

				err = conn.Connect()
				assert.NoError(t, err)

				err = conn.DropTable(srcTable)
				assert.NoError(t, err)

				err = conn.DropTable(srcTableCopy)
				assert.NoError(t, err)

				err = conn.Close()
				assert.NoError(t, err)
			}
		} else {
			testFile1Lines := len(strings.Split(string(testFile1Bytes), "\n"))
			testFile2Lines := len(strings.Split(string(testFile2Bytes), "\n"))
			equal := assert.Equal(t, testFile1Lines, testFile2Lines)

			if equal {
				err = os.Remove(filePath2)
				os.Remove(strings.ReplaceAll(srcDB.URL, "file:", ""))
			} else {
				println("Not equal for " + srcDB.name)
			}
		}
	}
}
