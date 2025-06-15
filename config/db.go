package config

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func InitDB() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL not set in environment")
	}
	var err error
	DB, err = sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	return DB.Ping()
}
