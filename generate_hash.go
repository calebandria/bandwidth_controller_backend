package main

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	// Generate a new hash for a known password
	password := "admin123"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("Error generating hash: %v\n", err)
		return
	}

	fmt.Println("Generated bcrypt hash for password 'admin123':")
	fmt.Println(string(hash))
	fmt.Println()
	fmt.Println("Use this SQL to update your user:")
	fmt.Printf("UPDATE users SET password_hash = '%s', updated_at = now() WHERE username = 'kaleba';\n", string(hash))
	fmt.Println()

	// Verify it works
	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	if err == nil {
		fmt.Println("✓ Verification successful - password 'admin123' matches this hash")
	} else {
		fmt.Println("✗ Verification failed")
	}
}
