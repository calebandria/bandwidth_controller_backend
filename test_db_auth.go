package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	// Connect to DB
	connStr := "host=localhost port=5432 user=qos_user password=qos_mandeha_no_tanjona dbname=qos_db sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("DB connection error: %v", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("DB ping error: %v", err)
	}
	fmt.Println("✓ Connected to database")

	// Fetch user
	username := "kaleba"
	var dbUsername, passwordHash, role, name string
	var id int

	query := `SELECT id, username, password_hash, role, name FROM users WHERE username = $1`
	err = db.QueryRow(query, username).Scan(&id, &dbUsername, &passwordHash, &role, &name)
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Printf("\n✓ Found user in DB:\n")
	fmt.Printf("  ID: %d\n", id)
	fmt.Printf("  Username: %s\n", dbUsername)
	fmt.Printf("  Role: %s\n", role)
	fmt.Printf("  Name: %s\n", name)
	fmt.Printf("  Password Hash: %s\n", passwordHash)

	// Test password verification
	fmt.Println("\nTesting password verification:")
	testPasswords := []string{"admin123", "kaleba", "TsaraNyAndro", "admin"}

	for _, pw := range testPasswords {
		err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(pw))
		if err == nil {
			fmt.Printf("  ✓ Password '%s' MATCHES\n", pw)
		} else {
			fmt.Printf("  ✗ Password '%s' does not match: %v\n", pw, err)
		}
	}
}
