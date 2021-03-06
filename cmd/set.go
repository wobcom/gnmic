// Copyright © 2020 Karim Radhouani <medkarimrdi@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"

	"github.com/karimra/gnmic/collector"
	"github.com/karimra/gnmic/config"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// setCmd represents the set command
func newSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "run gnmi set on targets",
		Annotations: map[string]string{
			"--delete":       "XPATH",
			"--prefix":       "PREFIX",
			"--replace":      "XPATH",
			"--replace-file": "FILE",
			"--replace-path": "XPATH",
			"--update":       "XPATH",
			"--update-file":  "FILE",
			"--update-path":  "XPATH",
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cli.config.SetLocalFlagsFromFile(cmd)
			return cli.config.ValidateSetInput()
		},
		RunE: cli.setRunE,
		PostRun: func(cmd *cobra.Command, args []string) {
			cmd.ResetFlags()
			initSetFlags(cmd)
		},
		SilenceUsage: true,
	}
	initSetFlags(cmd)
	return cmd
}

func (c *CLI) setRequest(ctx context.Context, tName string, req *gnmi.SetRequest) {
	defer c.wg.Done()
	c.logger.Printf("sending gNMI SetRequest: prefix='%v', delete='%v', replace='%v', update='%v', extension='%v' to %s",
		req.Prefix, req.Delete, req.Replace, req.Update, req.Extension, tName)
	if c.config.Globals.PrintRequest {
		err := c.printMsg(tName, "Set Request:", req)
		if err != nil {
			c.logger.Printf("target %s: %v", tName, err)
			if !c.config.Globals.Log {
				fmt.Printf("target %s: %v\n", tName, err)
			}
		}
	}
	response, err := c.collector.Set(ctx, tName, req)
	if err != nil {
		c.logger.Printf("error sending set request: %v", err)
		return
	}
	err = c.printMsg(tName, "Set Response:", response)
	if err != nil {
		c.logger.Printf("%v", err)
		if !c.config.Globals.Log {
			fmt.Printf("%v\n", err)
		}
	}
}

// used to init or reset setCmd flags for gnmic-prompt mode
func initSetFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("prefix", "", "", "set request prefix")

	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetDelete, "delete", "", []string{}, "set request path to be deleted")

	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetReplace, "replace", "", []string{}, fmt.Sprintf("set request path:::type:::value to be replaced, type must be one of %v", config.ValueTypes))
	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetUpdate, "update", "", []string{}, fmt.Sprintf("set request path:::type:::value to be updated, type must be one of %v", config.ValueTypes))

	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetReplacePath, "replace-path", "", []string{}, "set request path to be replaced")
	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetUpdatePath, "update-path", "", []string{}, "set request path to be updated")
	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetUpdateFile, "update-file", "", []string{}, "set update request value in json/yaml file")
	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetReplaceFile, "replace-file", "", []string{}, "set replace request value in json/yaml file")
	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetUpdateValue, "update-value", "", []string{}, "set update request value")
	cmd.Flags().StringArrayVarP(&cli.config.LocalFlags.SetReplaceValue, "replace-value", "", []string{}, "set replace request value")
	cmd.Flags().StringVarP(&cli.config.LocalFlags.SetDelimiter, "delimiter", "", ":::", "set update/replace delimiter between path, type, value")
	cmd.Flags().StringVarP(&cli.config.LocalFlags.SetTarget, "target", "", "", "set request target")

	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		cli.config.FileConfig.BindPFlag(fmt.Sprintf("%s-%s", cmd.Name(), flag.Name), flag)
	})
}

func (c *CLI) setRunE(cmd *cobra.Command, args []string) error {
	if c.config.Globals.Format == "event" {
		return fmt.Errorf("format event not supported for Set RPC")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupCloseHandler(cancel)
	targetsConfig, err := c.config.GetTargets()
	if err != nil {
		return fmt.Errorf("failed getting targets config: %v", err)
	}
	if len(targetsConfig) > 1 {
		fmt.Println("[warning] running set command on multiple targets")
	}
	if c.collector == nil {
		cfg := &collector.Config{
			Debug:               c.config.Globals.Debug,
			Format:              c.config.Globals.Format,
			TargetReceiveBuffer: c.config.Globals.TargetBufferSize,
			RetryTimer:          c.config.Globals.Retry,
		}

		c.collector = collector.NewCollector(cfg, targetsConfig,
			collector.WithDialOptions(createCollectorDialOpts()),
			collector.WithLogger(c.logger),
		)
	} else {
		// prompt mode
		for _, tc := range targetsConfig {
			c.collector.AddTarget(tc)
		}
	}
	req, err := c.config.CreateSetRequest()
	if err != nil {
		return err
	}

	c.wg.Add(len(c.collector.Targets))
	for tName := range c.collector.Targets {
		go c.setRequest(ctx, tName, req)
	}
	c.wg.Wait()
	return nil
}
