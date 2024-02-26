/*
Copyright AppsCode Inc. and Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmds

import (
	"github.com/kluster-manager/cluster-auth/pkg/cmds/agent"
	"github.com/kluster-manager/cluster-auth/pkg/cmds/manager"

	"github.com/spf13/cobra"
	v "gomodules.xyz/x/version"
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:               "cluster-auth [command]",
		Short:             `OCM Cluster Auth Addon`,
		DisableAutoGenTag: true,
	}

	rootCmd.AddCommand(v.NewCmdVersion())
	rootCmd.AddCommand(agent.NewCmdAgent())
	rootCmd.AddCommand(manager.NewCmdManager())

	return rootCmd
}
