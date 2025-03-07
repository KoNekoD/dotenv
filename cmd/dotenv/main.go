package main

import (
	"fmt"
	"github.com/KoNekoD/dotenv/pkg/dotenv"
	"os"
)

func main() {
	if err := os.Setenv("AGE", "18"); err != nil {
		panic(err)
	}

	err := dotenv.LoadEnv(".env")
	if err != nil {
		panic(err)
	}

	fmt.Printf("APP_ENV: %s\n", os.Getenv("APP_ENV"))
	fmt.Printf("NAME: %s\n", os.Getenv("NAME"))
	fmt.Printf("AGE: %s\n", os.Getenv("AGE"))
}
