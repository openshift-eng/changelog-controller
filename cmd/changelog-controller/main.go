package main

import (
	"os"

	changelogcontroller "github.com/openshift-eng/changelog-controller/pkg/cmd/changelog-controller"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"
)

func main() {
	command := NewReleasePayloadChangelogControllerCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewReleasePayloadChangelogControllerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release-payload-changelog-controller",
		Short: "OpenShift Release Payload Changelog Controller",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	cmd.AddCommand(changelogcontroller.NewReleasePayloadChangelogControllerCommand("start"))
	return cmd
}
