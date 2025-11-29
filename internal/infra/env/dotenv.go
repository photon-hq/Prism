package env

import (
	"log"
	"os"
	"sync"

	"github.com/joho/godotenv"
)

var loadOnce sync.Once

func Load() {
	loadOnce.Do(func() {
		if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
			log.Printf("[env] WARN: failed to load .env: %v", err)
		}
	})
}
