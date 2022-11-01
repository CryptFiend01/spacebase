package db

import (
	"database/sql"

	// _ "github.com/lib/pq"
	_ "github.com/go-sql-driver/mysql"
	"github.com/wonderivan/logger"
)

var (
	db *sql.DB
)

func Query(sqlSeq string) *sql.Rows {
	rows, err := db.Query(sqlSeq)
	if err != nil {
		logger.Error("Query sql(%s) failed, errr=%v", sqlSeq, err)
		return nil
	}
	return rows
}

func ExecSql(sqlSeq string) bool {
	logger.Debug("exec sql: %v", sqlSeq)
	_, err := db.Exec(sqlSeq)
	if err != nil {
		logger.Error("Exec sql %s failed! err=%v", sqlSeq, err)
		return false
	}
	return true
}

func QueryOne(sqlSeq string) *sql.Row {
	return db.QueryRow(sqlSeq)
}

func QueryFloat(sqlSeq string) (bool, float64) {
	var num sql.NullFloat64

	row := db.QueryRow(sqlSeq)
	err := row.Scan(&num)
	if err == sql.ErrNoRows {
		return false, -1
	} else if err != nil {
		logger.Error("query sql(%s) erorr: %v", sqlSeq, err)
		return false, 0
	}

	return true, num.Float64
}

func QueryInt(sqlSeq string) (bool, int) {
	var num sql.NullInt32

	row := db.QueryRow(sqlSeq)
	err := row.Scan(&num)
	if err == sql.ErrNoRows {
		return false, -1
	} else if err != nil {
		logger.Error("query sql(%s) erorr: %v", sqlSeq, err)
		return false, 0
	}

	return true, int(num.Int32)
}

func ConnectDB(driveName string, dbInfo string, maxOpenConn int) *sql.DB {
	var err error
	db, err := sql.Open(driveName, dbInfo)
	if err != nil {
		logger.Error("Connect db %s failed! err: %v", dbInfo, err)
		return nil
	}

	db.SetMaxOpenConns(maxOpenConn)
	return db
}

func ExecMultiSql(sqls []string) bool {
	tx, err := db.Begin()
	if err != nil {
		logger.Error("ExecMultiSql begin tx failed, err: %v", err)
		for _, sqlSeq := range sqls {
			logger.Debug("not run sql: %s", sqlSeq)
		}
		return false
	}

	hasErr := false
	for _, sqlSeq := range sqls {
		_, err = tx.Exec(sqlSeq)
		if err != nil {
			logger.Error("Exec sql (%s) error: %v", sqlSeq, err)
			hasErr = true
		} else {
			logger.Debug("exec sql: %v", sqlSeq)
		}
	}
	err = tx.Commit()
	if err != nil {
		logger.Error("Commit sqls error: %v", err)
		return false
	}
	return !hasErr
}

func Init(dbInfo string) bool {
	// db = ConnectDB("postgres", dbInfo)
	db = ConnectDB("mysql", dbInfo, 5)
	if db == nil {
		logger.Error("Connect db failed!")
		return false
	}

	return true
}

func InitOpenConns(dbInfo string, maxOpenConn int) bool {
	// db = ConnectDB("postgres", dbInfo)
	db = ConnectDB("mysql", dbInfo, maxOpenConn)
	if db == nil {
		logger.Error("Connect db failed!")
		return false
	}

	return true
}
