package main

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	// The hash currently in DB
	currentHash := "$2a$10$CLxnjLvw8R2Qyc5dMlwMSuYFPkv/n1990F6SWH6b9cZVraoOax80q"

	// The hash you originally tried to insert
	originalHash := "$2a$10$ifYf9u.EcWmhksF2CtGgFu.KY6YB2tVXgzEd8La7QMxFCBRCAwzg6"

	passwords := []string{"admin123", "kaleba", "Tompony", "qos_mandeha_no_tanjona", "admin", "YourAdminPassword"}

	fmt.Println("Testing CURRENT hash in database:")
	fmt.Println("Hash:", currentHash)
	fmt.Println()

	for _, pw := range passwords {
		err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(pw))
		if err == nil {
			fmt.Printf("✓ Password matches: '%s'\n", pw)
		} else {
			fmt.Printf("✗ Password does not match: '%s'\n", pw)
		}
	}

	fmt.Println("\n============================================================")
	fmt.Println("Testing ORIGINAL hash you tried to insert:")
	fmt.Println("Hash:", originalHash)
	fmt.Println()

	for _, pw := range passwords {
		err := bcrypt.CompareHashAndPassword([]byte(originalHash), []byte(pw))
		if err == nil {
			fmt.Printf("✓ Password matches: '%s'\n", pw)
		} else {
			fmt.Printf("✗ Password does not match: '%s'\n", pw)
		}
	}
}
