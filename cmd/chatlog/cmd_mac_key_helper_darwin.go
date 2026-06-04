//go:build darwin

package chatlog

import (
	"encoding/json"
	"fmt"
	"os"

	darwinkey "github.com/sjzar/chatlog/internal/wechat/key/darwin"
	"github.com/spf13/cobra"
)

var macKeyHelperPID uint32
var macKeyHelperDataDir string
var macKeyHelperSaltFile string
var macKeyHelperOutJSON string

func init() {
	cmd := &cobra.Command{
		Use:    "mac-key-helper",
		Hidden: true,
		Short:  "scan macOS WeChat keys with elevated privileges",
		RunE: func(cmd *cobra.Command, args []string) error {
			if macKeyHelperSaltFile != "" {
				raw, err := os.ReadFile(macKeyHelperSaltFile)
				if err != nil {
					return err
				}
				salts := map[string]string{}
				if err := json.Unmarshal(raw, &salts); err != nil {
					return err
				}
				keys, key, err := darwinkey.BuildAllKeysByPIDAndSalts(macKeyHelperPID, salts)
				if err != nil {
					return err
				}
				if macKeyHelperOutJSON != "" {
					out := make(map[string]map[string]string, len(keys))
					for rel, encKey := range keys {
						out[rel] = map[string]string{"enc_key": encKey}
					}
					b, err := json.MarshalIndent(out, "", "  ")
					if err != nil {
						return err
					}
					if err := os.WriteFile(macKeyHelperOutJSON, append(b, '\n'), 0600); err != nil {
						return err
					}
				}
				fmt.Println(key)
				return nil
			}
			key, _, err := darwinkey.InitAllKeysByPID(macKeyHelperPID, macKeyHelperDataDir, nil)
			if err != nil {
				return err
			}
			fmt.Println(key)
			return nil
		},
	}
	cmd.Flags().Uint32Var(&macKeyHelperPID, "pid", 0, "WeChat process PID")
	cmd.Flags().StringVar(&macKeyHelperDataDir, "data-dir", "", "WeChat account data directory")
	cmd.Flags().StringVar(&macKeyHelperSaltFile, "salt-file", "", "JSON map of relative db path to salt hex")
	cmd.Flags().StringVar(&macKeyHelperOutJSON, "out-json", "", "write all_keys JSON to this path")
	rootCmd.AddCommand(cmd)
}
