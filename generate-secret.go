
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func main() {
	secret := make([]byte, 32)

	if _, err := rand.Read(secret); err != nil {
		panic(err)
	}

	fmt.Println(hex.EncodeToString(secret))
}
