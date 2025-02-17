package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "vec",
	Short: "Vec is a simplified, distributed version control system",
	Long: `Vec is a simplified, distributed version control system designed for Bahirdar Institute of Technology
	faculty of computing project repository platform.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
