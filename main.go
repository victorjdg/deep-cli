package main

import (
	"github.com/victorjdg/deep-cli/cmd"
	"github.com/joho/godotenv"
)

// Version is set via ldflags at build time.
var Version = "dev"

func main() {
	_ = godotenv.Load()
	cmd.SetVersion(Version)
	cmd.Execute()
}
