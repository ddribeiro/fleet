package tables

import (
	"database/sql"
	"fmt"
)

func init() {
	MigrationClient.AddMigration(Up_20231004144338, Down_20231004144338)
}

func Up_20231004144338(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE query_results (
			id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
			query_id INT(10) UNSIGNED NOT NULL,
			host_id INT(10) UNSIGNED NOT NULL,
			osquery_version VARCHAR(50),
			error TEXT COLLATE utf8mb4_unicode_ci DEFAULT NULL,
			last_fetched TIMESTAMP NOT NULL,
			data JSON,
			FOREIGN KEY (query_id) REFERENCES queries(id) ON DELETE CASCADE
		);
    `)
	if err != nil {
		return fmt.Errorf("failed to create table query_results: %w", err)
	}

	return nil
}

func Down_20231004144338(tx *sql.Tx) error {
	return nil
}
