package chatlog

import (
	"github.com/rs/zerolog/log"
	core "github.com/sjzar/chatlog/internal/chatlog"
	"github.com/spf13/cobra"
)

var serveConfigDir string

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVar(&serveConfigDir, "config-dir", "", "config directory containing chatlog-server.json")
}

var serveCmd = &cobra.Command{
	Use:    "serve",
	Short:  "Start HTTP server from service config",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		m := core.New()
		if err := m.CommandHTTPServer(serveConfigDir, nil); err != nil {
			log.Err(err).Msg("failed to start chatlog HTTP server")
		}
	},
}
